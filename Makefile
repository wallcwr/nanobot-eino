.PHONY: langfuse-up langfuse-down langfuse-logs

langfuse-up: ## Start Langfuse (auto-installs Docker if needed)
	@bash scripts/start-langfuse.sh

langfuse-down: ## Stop Langfuse
	@bash scripts/stop-langfuse.sh

langfuse-logs: ## Follow Langfuse logs
	@docker compose -f docker-compose.langfuse.yml logs -f
