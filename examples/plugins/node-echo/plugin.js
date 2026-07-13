#!/usr/bin/env node
"use strict";
const readline = require("readline");
const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
function reply(id, result, error) {
  const message = { jsonrpc: "2.0", id };
  if (error) message.error = error; else message.result = result;
  process.stdout.write(JSON.stringify(message) + "\n");
}
rl.on("line", (line) => {
  try {
    const req = JSON.parse(line), p = req.params || {};
    if (req.id === undefined) return;
    if (req.method === "plugin.initialize") reply(req.id, { protocol_version: "1.0", name: "node-echo", version: "0.1.0", capabilities: { tools: true, cancellation: true, health: true, max_concurrency: 1 } });
    else if (req.method === "plugin.health") reply(req.id, { status: "ok" });
    else if (req.method === "tools.list") reply(req.id, { tools: [{ name: "echo", description: "Echo text", input_schema: { type: "object", properties: { text: { type: "string" } }, required: ["text"], additionalProperties: false } }] });
    else if (req.method === "tools.execute") p.name === "echo" ? reply(req.id, { output: String((p.arguments || {}).text || ""), is_error: false }) : reply(req.id, null, { code: -32003, message: "tool not found" });
    else if (req.method === "plugin.shutdown") { reply(req.id, {}); process.exit(0); }
    else reply(req.id, null, { code: -32601, message: "method not found" });
  } catch (err) { reply(null, null, { code: -32700, message: err.message }); }
});
