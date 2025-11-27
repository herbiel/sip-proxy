#!/bin/bash
# Test script to simulate multiple callback requests

echo "Testing callback endpoint with multiple requests..."
echo ""

for i in {1..5}; do
    echo "=== Request $i ==="
    curl -sS -m 5 -X POST http://127.0.0.1:3000/callback \
        -H 'Content-Type: application/json' \
        -d "{\"uri\":\"sip:alice@192.168.50.71\",\"from\":\"<sip:alice@192.168.50.71>\",\"call_id\":\"test-call-$i\"}" \
        -w '\nHTTP_STATUS:%{http_code}\n' | head -n 3
    echo ""
    sleep 0.5
done

echo ""
echo "=== Checking callback server log for all requests ==="
tail -n 20 callback.log | grep -E 'POST /callback|Callback request' || echo "No requests logged"
