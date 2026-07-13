# Plugin protocol v1

Plugins communicate over stdio using JSON-RPC 2.0. Each UTF-8 line on stdout is exactly one complete JSON object; embedded newlines are escaped. Stdout must contain no banners or logs. Stderr is for bounded diagnostic logs.

## Lifecycle

1. GoHermit starts the configured child in the workspace.
2. `plugin.initialize` exchanges `protocol_version`, identity, capabilities, concurrency, and maximum message size.
3. `tools.list` discovers definitions.
4. `tools.execute` runs a named tool with `request_id`, JSON arguments, and timeout.
5. `tools.cancel` is sent when the caller is cancelled.
6. `plugin.health` returns `{ "status": "ok" }`.
7. `plugin.shutdown` requests graceful exit; the supervisor kills the process after the shutdown deadline.

Tools are registered as `plugin.<configured-name>.<tool-name>`. Definitions include name, description, JSON Schema, optional permission (`read`, `write`, or `execute`), workspace-mutation declaration, and default timeout.

## Messages

```json
{"jsonrpc":"2.0","id":"1","method":"plugin.initialize","params":{"protocol_version":"1.0","client_name":"GoHermit","max_message_size":4194304}}
{"jsonrpc":"2.0","id":"1","result":{"protocol_version":"1.0","name":"echo","version":"0.1.0","capabilities":{"tools":true,"cancellation":true,"health":true,"max_concurrency":1}}}
```

Requests and responses are correlated by JSON-RPC `id`. Long tool work also receives an application `request_id` used for cancellation. Streaming event extensions are not enabled in v0.1; a plugin must return one bounded final tool response.

## Errors

Standard JSON-RPC errors are `-32700` parse, `-32600` invalid request, `-32601` method not found, `-32602` invalid params, and `-32603` internal. GoHermit reserves `-32001` cancelled, `-32002` timeout, `-32003` tool not found, and `-32004` message too large.

Invalid JSON, oversized messages, stdout closure, abnormal exit, timeout, and cancellation are isolated to the plugin client and returned as tool/runtime errors. Pending calls are released when a plugin dies.

## Examples

`examples/plugins/python-echo/plugin.py` and `examples/plugins/node-echo/plugin.js` use only their language standard libraries. Configure either through `[[plugins.process]]`; see the README.
