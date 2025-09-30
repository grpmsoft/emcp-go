# eMCP SDK TODO

## ✅ Completed (v0.1.0)

- [x] Core types with MCP 1.0 compatibility
- [x] Server implementation with stdio transport
- [x] Client implementation with stdio transport
- [x] Middleware support (logging, recovery, timeout, metrics)
- [x] Working examples (basic server + client-demo)
- [x] Full client-server communication tested

## 🚀 Next Steps

### High Priority

- [ ] **Add HTTP/SSE transport** - для WebSocket/HTTP клиентов
  - server/http.go
  - client/http.go
  - Поддержка Server-Sent Events

- [ ] **gRPC transport** - для высокопроизводительных сценариев
  - proto/emcp.proto
  - server/grpc.go
  - client/grpc.go

- [ ] **Checkpoint support** - eMCP core feature
  - emcp/checkpoint.go - типы
  - server/checkpoint.go - сервер-сайд
  - client/checkpoint.go - клиент-сайд
  - Интеграция с существующим GODA checkpoint system

### Medium Priority

- [ ] **Testing**
  - Unit tests для всех пакетов (минимум 70% coverage)
  - Integration tests для client-server
  - Benchmark tests для производительности

- [ ] **Documentation**
  - GoDoc комментарии для всех публичных API
  - Примеры использования в документации
  - Architectural decision records (ADR)

- [ ] **Client improvements**
  - Retry logic с exponential backoff
  - Connection pooling
  - Request timeout configuration

- [ ] **Server improvements**
  - Resource support (MCP resources protocol)
  - Prompts support (MCP prompts protocol)
  - Sampling support (MCP sampling protocol)

### Low Priority

- [ ] **Advanced features**
  - Rate limiting middleware
  - Authentication/Authorization
  - Request/response logging
  - Metrics export (Prometheus format)

- [ ] **Developer experience**
  - CLI tool для scaffolding (create new server/client)
  - Live reload для разработки
  - Debug mode с подробными логами

## 🔧 GoCo Integration Tasks

### High Priority

- [ ] **JSON output для GoCo CLI** (как в Claude Code)
  - `goco chat --json` - вывод в JSON
  - `goco daemon status --json` - статус в JSON
  - `goco version --json` - версия в JSON
  - Унифицированный формат ответов

- [ ] **MCP integration в GoCo daemon**
  - Использовать emcp-go/server
  - Регистрация GoCo-специфичных tools
  - Интеграция с существующим LLM функционалом

- [ ] **GoCo MCP tools**
  - `goco_ask` - задать вопрос агенту
  - `goco_agent_create` - создать нового агента
  - `goco_daemon_status` - получить статус демона
  - `goco_conversation_history` - история разговора

### Medium Priority

- [ ] **Checkpoint integration**
  - Автоматические checkpoints перед рискованных операций
  - Восстановление из checkpoint при ошибках
  - CLI команды для управления checkpoints

- [ ] **Agent collaboration через MCP**
  - Агенты как MCP серверы
  - Межагентная коммуникация через MCP протокол
  - Shared context между агентами

## 📚 Documentation Tasks

- [ ] **API Reference** - полная документация API
- [ ] **Tutorial** - step-by-step гайд
- [ ] **Examples** - больше примеров использования
- [ ] **Architecture** - документация архитектуры
- [ ] **Migration Guide** - для перехода с других MCP SDK

## 🔬 Research

- [ ] **Performance benchmarks** vs другие MCP implementations
- [ ] **Security audit** - проверка безопасности
- [ ] **Compatibility testing** - тестирование с разными MCP клиентами
- [ ] **Load testing** - нагрузочное тестирование

---

**Версия**: 0.1.0
**Последнее обновление**: 30 сентября 2025
**Статус**: Active Development