package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/wall/nanobot-eino/pkg/bus"
)

var logFeishu = slog.With("module", "feishu")

type FeishuChannel struct {
	client *lark.Client
	bus    *bus.MessageBus
	config FeishuConfig

	// 消息去重缓存
	processedMsgs sync.Map

	lifecycleMu sync.Mutex
	stopWS      context.CancelFunc
	wsDone      chan struct{}
}

const feishuCardMarkdownMaxRunes = 3000

type FeishuConfig struct {
	AppID             string
	AppSecret         string
	VerificationToken string
	EncryptKey        string
	AllowFrom         []string
	GroupPolicy       string
}

func NewFeishuChannel(cfg FeishuConfig, messageBus *bus.MessageBus) *FeishuChannel {
	client := lark.NewClient(cfg.AppID, cfg.AppSecret)
	return &FeishuChannel{
		client: client,
		bus:    messageBus,
		config: cfg,
	}
}

func (c *FeishuChannel) Start(ctx context.Context) error {
	c.lifecycleMu.Lock()
	if c.stopWS != nil {
		c.lifecycleMu.Unlock()
		return nil
	}
	wsCtx, wsCancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.stopWS = wsCancel
	c.wsDone = done
	c.lifecycleMu.Unlock()

	// 1. 定义事件处理器
	handler := larkevent.NewEventDispatcher(c.config.VerificationToken, c.config.EncryptKey).
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			return c.onMessage(ctx, event)
		})

	// 2. 初始化 WebSocket 客户端
	wsClient := larkws.NewClient(c.config.AppID, c.config.AppSecret,
		larkws.WithEventHandler(handler),
	)

	// 3. 异步启动
	go func() {
		defer close(done)
		err := wsClient.Start(wsCtx)
		if err != nil && wsCtx.Err() == nil {
			logFeishu.Error("WebSocket error", "error", err)
			return
		}
	}()

	logFeishu.Info("Channel started with WebSocket")
	return nil
}

func (c *FeishuChannel) Stop(ctx context.Context) error {
	c.lifecycleMu.Lock()
	cancel := c.stopWS
	done := c.wsDone
	c.stopWS = nil
	c.wsDone = nil
	c.lifecycleMu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()

	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *FeishuChannel) onMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	msg := event.Event.Message
	msgID := *msg.MessageId
	senderID := *event.Event.Sender.SenderId.OpenId

	// 1. 去重逻辑
	if _, loaded := c.processedMsgs.LoadOrStore(msgID, true); loaded {
		return nil
	}

	if !IsSenderAllowed("feishu", senderID, c.config.AllowFrom) {
		return nil
	}

	// 2. 提取内容
	content := *msg.Content
	msgType := *msg.MessageType

	logFeishu.Debug("Raw content", "content", content, "msg_type", msgType)

	var contentJSON map[string]any
	if jsonErr := json.Unmarshal([]byte(content), &contentJSON); jsonErr != nil {
		contentJSON = nil
	}

	var parsedContent string
	switch msgType {
	case "text":
		if contentJSON != nil {
			if text, ok := contentJSON["text"].(string); ok {
				parsedContent = text
			}
		}
		if parsedContent == "" {
			parsedContent = content
		}

	case "post":
		parsedContent = extractPostText(contentJSON)
		if parsedContent == "" {
			parsedContent = fmt.Sprintf("[%s]", msgType)
		}

	case "share_chat", "share_user", "interactive", "share_calendar_event", "system", "merge_forward":
		parsedContent = extractShareCardContent(contentJSON, msgType)

	default:
		parsedContent = fmt.Sprintf("[%s]", msgType)
	}

	rawParsedContent := strings.TrimSpace(parsedContent)

	logFeishu.Debug("Parsed content", "content", rawParsedContent)

	chatType := ""
	if msg.ChatType != nil {
		chatType = *msg.ChatType
	}
	if chatType == "group" && !shouldProcessGroupMessage(c.config.GroupPolicy, rawParsedContent) {
		return nil
	}

	// 处理飞书 @ 机器人的文本前缀
	parsedContent = normalizeFeishuText(rawParsedContent)

	// 3. 发布到总线
	c.bus.PublishInbound(ctx, &bus.InboundMessage{
		Channel:  "feishu",
		SenderID: senderID,
		ChatID:   *msg.ChatId,
		Content:  parsedContent,
		Metadata: map[string]any{
			"message_id": msgID,
		},
	})

	return nil
}

func (c *FeishuChannel) ListenOutbound(ctx context.Context) {
	outbound := c.bus.ConsumeOutbound(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-outbound:
			if !ok {
				return
			}
			if msg.Channel == "feishu" {
				isProgress := isFeishuProgressMessage(msg.Metadata)
				if isProgress {
					continue
				}
				err := c.SendMessage(ctx, msg.ChatID, msg.Content, msg.ReplyTo, msg.Metadata)
				if err != nil {
					logFeishu.Error("Failed to send message", "error", err)
				}
			}
		}
	}
}

func isFeishuProgressMessage(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	isProgress, _ := metadata["_progress"].(bool)
	return isProgress
}

func (c *FeishuChannel) Send(ctx context.Context, chatID, content string) error {
	return c.SendMessage(ctx, chatID, content, "", nil)
}

func (c *FeishuChannel) SendMessage(ctx context.Context, chatID, content, replyTo string, metadata map[string]any) error {
	cardContents, ok := buildFeishuCardContents(content, metadata)
	if !ok {
		return nil
	}

	replyTarget := pickFeishuReplyTarget(replyTo, metadata)
	if replyTarget != "" {
		replyReq := larkim.NewReplyMessageReqBuilder().
			MessageId(replyTarget).
			Body(larkim.NewReplyMessageReqBodyBuilder().
				MsgType(larkim.MsgTypeInteractive).
				Content(cardContents[0]).
				ReplyInThread(true).
				Build()).
			Build()

		replyResp, err := c.client.Im.Message.Reply(ctx, replyReq)
		if err == nil && replyResp.Success() {
			for i := 1; i < len(cardContents); i++ {
				if err := c.sendCreateCard(ctx, chatID, cardContents[i]); err != nil {
					return err
				}
			}
			return nil
		}
		if err != nil {
			logFeishu.Warn("Reply failed, fallback to create", "error", err)
		} else {
			logFeishu.Warn("Reply failed, fallback to create", "msg", replyResp.Msg)
		}
	}

	for _, cc := range cardContents {
		if err := c.sendCreateCard(ctx, chatID, cc); err != nil {
			return err
		}
	}
	return nil
}

func (c *FeishuChannel) sendCreateCard(ctx context.Context, chatID, cardContent string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(larkim.MsgTypeInteractive).
			Content(cardContent).
			Build()).
		Build()

	resp, err := c.client.Im.Message.Create(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu send failed: %s", resp.Msg)
	}
	return nil
}

func buildFeishuCardContents(content string, metadata map[string]any) ([]string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, false
	}

	rendered := trimmed
	if metadata != nil {
		if isToolHint, _ := metadata["_tool_hint"].(bool); isToolHint {
			rendered = formatToolHintMarkdown(trimmed)
		}
	}

	rendered = convertMarkdownToFeishu(rendered)

	chunks := splitFeishuMarkdownContent(rendered, feishuCardMarkdownMaxRunes)
	out := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		card := map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"elements": []map[string]any{
				{"tag": "markdown", "content": chunk},
			},
		}
		b, err := json.Marshal(card)
		if err != nil {
			escaped := strings.ReplaceAll(strings.ReplaceAll(chunk, `"`, `\"`), "\n", `\n`)
			out = append(out, fmt.Sprintf(`{"config":{"wide_screen_mode":true},"elements":[{"tag":"markdown","content":"%s"}]}`, escaped))
			continue
		}
		out = append(out, string(b))
	}
	return out, true
}

func buildFeishuCardContent(content string, metadata map[string]any) (string, bool) {
	contents, ok := buildFeishuCardContents(content, metadata)
	if !ok || len(contents) == 0 {
		return "", false
	}
	return contents[0], true
}

func formatToolHintMarkdown(content string) string {
	lines := splitToolHints(content)
	return "**Tool Calls**\n\n```text\n" + strings.Join(lines, "\n") + "\n```"
}

func splitToolHints(content string) []string {
	var lines []string
	var current strings.Builder
	inDoubleQuotes := false
	inSingleQuotes := false
	escapeNext := false

	for _, r := range content {
		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}
		if r == '\\' {
			current.WriteRune(r)
			escapeNext = true
			continue
		}
		if r == '"' && !inSingleQuotes {
			inDoubleQuotes = !inDoubleQuotes
			current.WriteRune(r)
			continue
		}
		if r == '\'' && !inDoubleQuotes {
			inSingleQuotes = !inSingleQuotes
			current.WriteRune(r)
			continue
		}
		if r == ',' && !inDoubleQuotes && !inSingleQuotes {
			current.WriteRune(r)
			lines = append(lines, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}

	last := strings.TrimSpace(current.String())
	if last != "" {
		lines = append(lines, last)
	}
	if len(lines) == 0 {
		return []string{strings.TrimSpace(content)}
	}
	return lines
}

func splitFeishuMarkdownContent(content string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = feishuCardMarkdownMaxRunes
	}
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return []string{content}
	}
	chunks := make([]string, 0, len(runes)/maxRunes+1)
	for start := 0; start < len(runes); start += maxRunes {
		end := start + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}
	return chunks
}

// convertMarkdownToFeishu transforms standard Markdown into the subset that
// Feishu card markdown supports.  Unsupported elements:
//   - Headings (# / ## / ###)  → bold text
//   - Tables (| col | col |)   → structured text with bold headers
//   - Blockquotes (> text)     → italic text
func convertMarkdownToFeishu(content string) string {
	lines := strings.Split(content, "\n")
	var out []string

	i := 0
	for i < len(lines) {
		line := lines[i]

		// --- headings → bold ---
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "#") {
			level := 0
			for _, ch := range trimmed {
				if ch == '#' {
					level++
				} else {
					break
				}
			}
			if level > 0 && level <= 6 {
				heading := strings.TrimSpace(trimmed[level:])
				out = append(out, "**"+heading+"**")
				i++
				continue
			}
		}

		// --- table block → structured text ---
		if isMarkdownTableRow(line) {
			tableStart := i
			for i < len(lines) && isMarkdownTableRow(lines[i]) {
				i++
			}
			converted := convertMarkdownTable(lines[tableStart:i])
			out = append(out, converted...)
			continue
		}

		// --- blockquotes → italic ---
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "> ") {
			quote := strings.TrimSpace(trimmed[2:])
			out = append(out, "*"+quote+"*")
			i++
			continue
		}

		out = append(out, line)
		i++
	}
	return strings.Join(out, "\n")
}

func isMarkdownTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|")
}

// convertMarkdownTable turns a Markdown table into a Feishu-friendly text block.
// Each data row becomes: **header1:** value1 | **header2:** value2 | …
func convertMarkdownTable(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}

	parseRow := func(line string) []string {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "|")
		trimmed = strings.TrimSuffix(trimmed, "|")
		cells := strings.Split(trimmed, "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		return cells
	}

	isSeparator := func(line string) bool {
		cells := parseRow(line)
		for _, c := range cells {
			cleaned := strings.ReplaceAll(strings.ReplaceAll(c, "-", ""), ":", "")
			if strings.TrimSpace(cleaned) != "" {
				return false
			}
		}
		return true
	}

	// Find header row and data rows, skipping separator.
	var headers []string
	var dataRows [][]string
	for idx, line := range lines {
		if isSeparator(line) {
			continue
		}
		cells := parseRow(line)
		if idx == 0 {
			headers = cells
		} else {
			dataRows = append(dataRows, cells)
		}
	}

	if len(headers) == 0 {
		return nil
	}

	var result []string
	// Emit headers as a bold line.
	result = append(result, "**"+strings.Join(headers, " | ")+"**")

	for _, row := range dataRows {
		var parts []string
		for j, cell := range row {
			if j < len(headers) {
				parts = append(parts, "**"+headers[j]+":** "+cell)
			} else {
				parts = append(parts, cell)
			}
		}
		result = append(result, strings.Join(parts, " | "))
	}
	return result
}

func pickFeishuReplyTarget(replyTo string, metadata map[string]any) string {
	if strings.TrimSpace(replyTo) != "" {
		return strings.TrimSpace(replyTo)
	}
	if metadata == nil {
		return ""
	}
	if isProgress, _ := metadata["_progress"].(bool); isProgress {
		return ""
	}
	if id, ok := metadata["message_id"].(string); ok {
		return strings.TrimSpace(id)
	}
	return ""
}


func shouldProcessGroupMessage(groupPolicy, content string) bool {
	policy := strings.TrimSpace(strings.ToLower(groupPolicy))
	if policy == "" {
		policy = "mention"
	}
	if policy == "open" {
		return true
	}
	return strings.Contains(content, "@_user_") || strings.Contains(strings.ToLower(content), "<at ")
}

func normalizeFeishuText(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "@_user_") {
		parts := strings.SplitN(trimmed, " ", 2)
		if len(parts) > 1 {
			return strings.TrimSpace(parts[1])
		}
		return ""
	}
	return trimmed
}

// ---------------------------------------------------------------------------
// Message content extraction helpers (mirrors nanobot's feishu.py parsers)
// ---------------------------------------------------------------------------

// extractShareCardContent converts share cards, interactive cards, and system
// message types into a human-readable string for the agent.
func extractShareCardContent(content map[string]any, msgType string) string {
	if content == nil {
		return fmt.Sprintf("[%s]", msgType)
	}
	switch msgType {
	case "share_chat":
		chatID, _ := content["chat_id"].(string)
		return fmt.Sprintf("[shared chat: %s]", chatID)
	case "share_user":
		userID, _ := content["user_id"].(string)
		return fmt.Sprintf("[shared user: %s]", userID)
	case "interactive":
		parts := extractInteractiveContent(content)
		if len(parts) == 0 {
			return "[interactive]"
		}
		return strings.Join(parts, "\n")
	case "share_calendar_event":
		key, _ := content["event_key"].(string)
		return fmt.Sprintf("[shared calendar event: %s]", key)
	case "system":
		return "[system message]"
	case "merge_forward":
		return "[merged forward messages]"
	default:
		return fmt.Sprintf("[%s]", msgType)
	}
}

// extractInteractiveContent recursively extracts text from an interactive card.
func extractInteractiveContent(content map[string]any) []string {
	if content == nil {
		return nil
	}
	var parts []string

	if title, ok := content["title"]; ok {
		switch t := title.(type) {
		case map[string]any:
			text, _ := t["content"].(string)
			if text == "" {
				text, _ = t["text"].(string)
			}
			if text != "" {
				parts = append(parts, "title: "+text)
			}
		case string:
			if t != "" {
				parts = append(parts, "title: "+t)
			}
		}
	}

	if elements, ok := content["elements"].([]any); ok {
		for _, row := range elements {
			switch r := row.(type) {
			case []any:
				for _, el := range r {
					if elem, ok := el.(map[string]any); ok {
						parts = append(parts, extractElementContent(elem)...)
					}
				}
			case map[string]any:
				parts = append(parts, extractElementContent(r)...)
			}
		}
	}

	if card, ok := content["card"].(map[string]any); ok {
		parts = append(parts, extractInteractiveContent(card)...)
	}

	if header, ok := content["header"].(map[string]any); ok {
		if headerTitle, ok := header["title"].(map[string]any); ok {
			text, _ := headerTitle["content"].(string)
			if text == "" {
				text, _ = headerTitle["text"].(string)
			}
			if text != "" {
				parts = append(parts, "title: "+text)
			}
		}
	}

	return parts
}

// extractElementContent extracts text from a single interactive card element.
func extractElementContent(element map[string]any) []string {
	if element == nil {
		return nil
	}
	var parts []string
	tag, _ := element["tag"].(string)

	switch tag {
	case "markdown", "lark_md":
		if c, _ := element["content"].(string); c != "" {
			parts = append(parts, c)
		}

	case "div":
		switch t := element["text"].(type) {
		case map[string]any:
			c, _ := t["content"].(string)
			if c == "" {
				c, _ = t["text"].(string)
			}
			if c != "" {
				parts = append(parts, c)
			}
		case string:
			if t != "" {
				parts = append(parts, t)
			}
		}
		if fields, ok := element["fields"].([]any); ok {
			for _, f := range fields {
				if field, ok := f.(map[string]any); ok {
					if ft, ok := field["text"].(map[string]any); ok {
						if c, _ := ft["content"].(string); c != "" {
							parts = append(parts, c)
						}
					}
				}
			}
		}

	case "a":
		if href, _ := element["href"].(string); href != "" {
			parts = append(parts, "link: "+href)
		}
		if text, _ := element["text"].(string); text != "" {
			parts = append(parts, text)
		}

	case "button":
		if t, ok := element["text"].(map[string]any); ok {
			if c, _ := t["content"].(string); c != "" {
				parts = append(parts, c)
			}
		}
		url, _ := element["url"].(string)
		if url == "" {
			if mu, ok := element["multi_url"].(map[string]any); ok {
				url, _ = mu["url"].(string)
			}
		}
		if url != "" {
			parts = append(parts, "link: "+url)
		}

	case "img":
		altText := "[image]"
		if alt, ok := element["alt"].(map[string]any); ok {
			if c, _ := alt["content"].(string); c != "" {
				altText = c
			}
		}
		parts = append(parts, altText)

	case "note":
		if elems, ok := element["elements"].([]any); ok {
			for _, e := range elems {
				if elem, ok := e.(map[string]any); ok {
					parts = append(parts, extractElementContent(elem)...)
				}
			}
		}

	case "column_set":
		if cols, ok := element["columns"].([]any); ok {
			for _, col := range cols {
				if c, ok := col.(map[string]any); ok {
					if elems, ok := c["elements"].([]any); ok {
						for _, e := range elems {
							if elem, ok := e.(map[string]any); ok {
								parts = append(parts, extractElementContent(elem)...)
							}
						}
					}
				}
			}
		}

	case "plain_text":
		if c, _ := element["content"].(string); c != "" {
			parts = append(parts, c)
		}

	default:
		if elems, ok := element["elements"].([]any); ok {
			for _, e := range elems {
				if elem, ok := e.(map[string]any); ok {
					parts = append(parts, extractElementContent(elem)...)
				}
			}
		}
	}

	return parts
}

// extractPostText extracts plain text from a Feishu rich-text (post) message.
// Handles three payload shapes: direct, localized, and wrapped.
func extractPostText(content map[string]any) string {
	if content == nil {
		return ""
	}

	// Unwrap optional {"post": ...} envelope
	if post, ok := content["post"].(map[string]any); ok {
		content = post
	}

	parseBlock := func(block map[string]any) string {
		if block == nil {
			return ""
		}
		rows, ok := block["content"].([]any)
		if !ok {
			return ""
		}
		var texts []string
		if title, _ := block["title"].(string); title != "" {
			texts = append(texts, title)
		}
		for _, row := range rows {
			rowSlice, ok := row.([]any)
			if !ok {
				continue
			}
			for _, el := range rowSlice {
				elem, ok := el.(map[string]any)
				if !ok {
					continue
				}
				switch elem["tag"] {
				case "text", "a":
					if t, _ := elem["text"].(string); t != "" {
						texts = append(texts, t)
					}
				case "at":
					name, _ := elem["user_name"].(string)
					if name == "" {
						name = "user"
					}
					texts = append(texts, "@"+name)
				}
			}
		}
		return strings.Join(texts, " ")
	}

	// Direct format
	if _, ok := content["content"]; ok {
		if t := parseBlock(content); t != "" {
			return t
		}
	}

	// Localized format
	for _, key := range []string{"zh_cn", "en_us", "ja_jp"} {
		if block, ok := content[key].(map[string]any); ok {
			if t := parseBlock(block); t != "" {
				return t
			}
		}
	}
	for _, val := range content {
		if block, ok := val.(map[string]any); ok {
			if t := parseBlock(block); t != "" {
				return t
			}
		}
	}

	return ""
}
