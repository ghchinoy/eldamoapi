#!/bin/bash
set -euo pipefail

# Eldamo MCP Server Post-Deployment Verification Script
# This script automates liveness, discovery, and OAuth JWT verification checks
# against the live deployed Cloud Run instance.

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPTS_DIR")"
ENV_FILE="$PROJECT_ROOT/.env"

# 1. Load deployment configuration from .env if present
GCP_PROJECT="testingproject-19c4c"
GCP_REGION="us-central1"
SERVICE_NAME="eldamo-mcp-server"

if [ -f "$ENV_FILE" ]; then
    echo "Loading configuration from .env..."
    # Export variables from .env without overriding manually set ones
    set -a
    # shellcheck disable=SC1090
    source "$ENV_FILE"
    set +a
fi

echo "========================================="
echo "   Eldamo MCP Server Post-Deploy Test    "
echo "========================================="

# 2. Resolve the deployed URL
echo "Querying gcloud for service URL..."
PROJECT_NUM=$(gcloud projects describe "$GCP_PROJECT" --format='value(projectNumber)' 2>/dev/null || echo "")
URLS=$(gcloud run services describe "$SERVICE_NAME" --region "$GCP_REGION" --project "$GCP_PROJECT" --format='value(metadata.annotations."run.googleapis.com/urls")' 2>/dev/null || echo "")

SERVICE_URL=""
if [ -n "$URLS" ]; then
    # Parse URLs using simple bash string manipulation to avoid jq dependency
    IFS=',' read -ra ADDR <<< "$URLS"
    for item in "${ADDR[@]}"; do
        clean_url=$(echo "$item" | tr -d '[]" ')
        if [ -n "$PROJECT_NUM" ] && [[ "$clean_url" == *"$PROJECT_NUM"* ]]; then
            SERVICE_URL="$clean_url"
            break
        fi
    done
fi

if [ -z "$SERVICE_URL" ]; then
    SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" \
        --region "$GCP_REGION" \
        --project "$GCP_PROJECT" \
        --format='value(status.url)' 2>/dev/null || "")
fi

if [ -z "$SERVICE_URL" ]; then
    echo "❌ Error: Could not resolve Cloud Run service URL."
    echo "Make sure you have gcloud authenticated and the service is deployed."
    exit 1
fi

echo "Resolved Deployed Service URL: $SERVICE_URL"
echo "========================================="

# 3. Check 1: Liveness & Discovery Probe
echo -n "Check 1: Liveness & Discovery Metadata... "
DISCOVERY_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$SERVICE_URL/.well-known/oauth-authorization-server")
if [ "$DISCOVERY_STATUS" -eq 200 ]; then
    echo "✅ PASS (Status 200)"
else
    echo "❌ FAIL (Status $DISCOVERY_STATUS)"
    exit 1
fi

# 4. Check 2: Active Authentication Layer (Require Token)
echo -n "Check 2: Auth Middleware Blocking (/sse without token)... "
UNAUTH_RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" "$SERVICE_URL/sse")
if [ "$UNAUTH_RESPONSE" -eq 401 ]; then
    echo "✅ PASS (Middleware active, returned 401 Unauthorized)"
else
    echo "❌ FAIL (Expected 401, but got $UNAUTH_RESPONSE)"
    exit 1
fi

# 5. Check 3: Valid Auth Access (JWT Verification)
echo "Check 3: Testing valid JWT access..."
echo "Generating secure access token..."
TOKEN=$(go run "$PROJECT_ROOT/scripts/gen-token/main.go")

if [ -z "$TOKEN" ]; then
    echo "❌ Error: Failed to generate local test token."
    exit 1
fi

echo "Sending authenticated request to SSE endpoint..."
# We use a standard GET request with a 3-second timeout to capture headers of the active stream
SSE_HEADER=$(curl -s -i \
    -H "Authorization: Bearer $TOKEN" \
    --max-time 3 \
    "$SERVICE_URL/sse" 2>/dev/null || true)

# Extract HTTP status code and content-type from the headers
HTTP_STATUS=$(echo "$SSE_HEADER" | grep -Ei '^HTTP' | awk '{print $2}' | tr -d '\r\n')
CONTENT_TYPE=$(echo "$SSE_HEADER" | grep -Ei '^content-type:' | awk '{print $2}' | tr -d '\r\n')

if [ "$HTTP_STATUS" = "200" ] && [[ "$CONTENT_TYPE" == *"text/event-stream"* ]]; then
    echo "✅ PASS (Server established secure SSE connection! Status: 200, Content-Type: $CONTENT_TYPE)"
else
    echo "❌ FAIL (Failed to authenticate or establish SSE stream)"
    echo "HTTP Status: ${HTTP_STATUS:-unknown}"
    echo "Content-Type: ${CONTENT_TYPE:-unknown}"
    exit 1
fi

echo "========================================="
echo "🎉 ALL POST-DEPLOY TESTS PASSED SUCCESSFULLY!"
echo "========================================="
