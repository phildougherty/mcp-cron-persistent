#!/bin/bash
# Test list_tasks MCP call

curl -s -X POST http://localhost:8018/sse \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2024-11-05",
      "capabilities": {},
      "clientInfo": {
        "name": "test-client",
        "version": "1.0.0"
      }
    }
  }' > /dev/null

sleep 1

curl -s -X POST http://localhost:8018/sse \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "list_tasks",
      "arguments": {}
    }
  }' | jq -r '.result.content[0].text' | jq '.[] | {id, name, mcpServers}'
