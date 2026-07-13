#!/usr/bin/env python3
import json
import sys

VERSION = "1.0"

def reply(request_id, result=None, error=None):
    message = {"jsonrpc": "2.0", "id": request_id}
    if error is not None:
        message["error"] = error
    else:
        message["result"] = result
    sys.stdout.write(json.dumps(message, separators=(",", ":")) + "\n")
    sys.stdout.flush()

for line in sys.stdin:
    try:
        req = json.loads(line)
        method = req.get("method")
        request_id = req.get("id")
        params = req.get("params") or {}
        if request_id is None:
            continue
        if method == "plugin.initialize":
            reply(request_id, {"protocol_version": VERSION, "name": "python-echo", "version": "0.1.0", "capabilities": {"tools": True, "cancellation": True, "health": True, "max_concurrency": 1}})
        elif method == "plugin.health":
            reply(request_id, {"status": "ok"})
        elif method == "tools.list":
            reply(request_id, {"tools": [{"name": "echo", "description": "Echo text", "input_schema": {"type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"], "additionalProperties": False}}]})
        elif method == "tools.execute":
            if params.get("name") != "echo":
                reply(request_id, error={"code": -32003, "message": "tool not found"})
            else:
                reply(request_id, {"output": str((params.get("arguments") or {}).get("text", "")), "is_error": False})
        elif method == "plugin.shutdown":
            reply(request_id, {})
            break
        else:
            reply(request_id, error={"code": -32601, "message": "method not found"})
    except Exception as exc:
        reply(None, error={"code": -32700, "message": str(exc)})
