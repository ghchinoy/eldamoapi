#!/bin/bash
set -euo pipefail

# Eldamo MCP Server Cloud Run Deployment Script
# Moves deployment configurations into a local .env file.
# Manages dedicated service accounts according to GCP security best practices.

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPTS_DIR")"

ENV_FILE="$PROJECT_ROOT/.env"

# Default fallback values
DEFAULT_GCP_PROJECT="testingproject-19c4c"
DEFAULT_REGION="us-central1"
DEFAULT_SERVICE_NAME="eldamo-mcp-server"

# Load .env file if it exists
if [ -f "$ENV_FILE" ]; then
    echo "Loading deployment configuration from .env..."
    # Export variables from .env
    set -a
    # shellcheck disable=SC1090
    source "$ENV_FILE"
    set +a
else
    echo "No .env file found in project root. Creating one with defaults..."
    RANDOM_JWT_KEY=$(openssl rand -hex 32 2>/dev/null || od -vN 32 -An -tx1 /dev/urandom | tr -d ' \n' | head -c 64)
    cat <<EOF > "$ENV_FILE"
# Google Cloud Platform Configuration
GCP_PROJECT=$DEFAULT_GCP_PROJECT
GCP_REGION=$DEFAULT_REGION
SERVICE_NAME=$DEFAULT_SERVICE_NAME

# Eldamo Security Configuration
# URL for the Elvish TTS Pronunciation Service
ELVISH_TTS_URL=

# Firebase & OAuth Configuration
FIREBASE_PROJECT_ID=$DEFAULT_GCP_PROJECT
FIREBASE_DATABASE=mithlond-services
JWT_SIGNING_KEY=$RANDOM_JWT_KEY
EOF
    echo "Created .env file at $ENV_FILE. Please configure your settings there."
    GCP_PROJECT="$DEFAULT_GCP_PROJECT"
    GCP_REGION="$DEFAULT_REGION"
    SERVICE_NAME="$DEFAULT_SERVICE_NAME"
fi

# Ensure OAuth defaults are set if not defined in sourced .env
FIREBASE_PROJECT_ID="${FIREBASE_PROJECT_ID:-$GCP_PROJECT}"
FIREBASE_DATABASE="${FIREBASE_DATABASE:-mithlond-services}"
if [ -z "${JWT_SIGNING_KEY:-}" ]; then
    echo "JWT_SIGNING_KEY not set. Generating a random key for deployment..."
    JWT_SIGNING_KEY=$(openssl rand -hex 32 2>/dev/null || od -vN 32 -An -tx1 /dev/urandom | tr -d ' \n' | head -c 64)
fi

echo "========================================="
echo " Deploying Eldamo MCP Server to Cloud Run "
echo "========================================="
echo "GCP Project:   $GCP_PROJECT"
echo "Region:        $GCP_REGION"
echo "Service Name:  $SERVICE_NAME"
echo "========================================="

# Set gcloud project context
gcloud config set project "$GCP_PROJECT" --quiet

# -----------------------------------------------------------------------------
# Dedicated Service Account Management
# -----------------------------------------------------------------------------
# GCP Security Best Practice: Use a fine-grained, dedicated Service Account
# with minimal/zero privileges instead of the broad default Compute Engine SA.
SERVICE_ACCOUNT_NAME="eldamo-mcp-runner"
SERVICE_ACCOUNT_EMAIL="$SERVICE_ACCOUNT_NAME@$GCP_PROJECT.iam.gserviceaccount.com"

echo "Checking for dedicated service account: $SERVICE_ACCOUNT_EMAIL..."
if ! gcloud iam service-accounts describe "$SERVICE_ACCOUNT_EMAIL" &>/dev/null; then
    echo "Service account not found. Creating $SERVICE_ACCOUNT_EMAIL..."
    gcloud iam service-accounts create "$SERVICE_ACCOUNT_NAME" \
        --description="Minimal privilege runner for the Eldamo MCP Server" \
        --display-name="Eldamo MCP Runner" \
        --quiet
    echo "✓ Successfully created service account."
else
    echo "✓ Service account exists."
fi

# roles/datastore.user — write/read Firestore (OAuth codes, authorized_users).
echo "Ensuring Datastore User role is bound to service account..."
gcloud projects add-iam-policy-binding "$GCP_PROJECT" \
    --member="serviceAccount:$SERVICE_ACCOUNT_EMAIL" \
    --role="roles/datastore.user" \
    --quiet &>/dev/null || echo "Warning: failed to bind datastore.user role (ensure you have project owner/admin permissions)."

# ── One-time Firestore setup for a2a_tasks ────────────────────────────────────
# Run these once after first deploy. Both commands are idempotent.
#
# 1. Composite index — required for List (WHERE user ORDER BY updatedAt):
#    gcloud firestore indexes composite create \
#      --project="$GCP_PROJECT" --database=mithlond-services \
#      --collection-group=a2a_tasks \
#      --field-config=field-path=user,order=ascending \
#      --field-config=field-path=updatedAt,order=descending
#
# 2. TTL policy — auto-expires task documents 7 days after expiresAt:
#    gcloud firestore fields ttls update expiresAt \
#      --collection-group=a2a_tasks \
#      --enable-ttl \
#      --database=mithlond-services \
#      --project="$GCP_PROJECT"
#
# Note: a2a_tasks collection is invisible in the Firebase console until the
# first A2A request creates a document (Firestore lazy collection creation).
# ─────────────────────────────────────────────────────────────────────────────

# roles/aiplatform.user — call Vertex AI (Gemini) for the translate skill.
# Only required when GEMINI_TRANSLATE_MODEL is set; binding is idempotent.
echo "Ensuring Vertex AI User role is bound to service account..."
gcloud projects add-iam-policy-binding "$GCP_PROJECT" \
    --member="serviceAccount:$SERVICE_ACCOUNT_EMAIL" \
    --role="roles/aiplatform.user" \
    --quiet &>/dev/null || echo "Warning: failed to bind aiplatform.user role (ensure you have project owner/admin permissions)."

# -----------------------------------------------------------------------------
# Build and Deploy
# -----------------------------------------------------------------------------
# GEMINI_LOCATION is the Vertex AI API location for Gemini skills (separate from
# GCP_REGION which is the Cloud Run deploy region). Newer models (gemini-3.x) use
# "global"; regional endpoints (us-central1) serve older model generations.
GEMINI_LOCATION="${GEMINI_LOCATION:-global}"
ENV_VARS="FIREBASE_PROJECT_ID=$FIREBASE_PROJECT_ID,FIREBASE_DATABASE=$FIREBASE_DATABASE,JWT_SIGNING_KEY=$JWT_SIGNING_KEY,GCP_PROJECT=$GCP_PROJECT,GEMINI_LOCATION=$GEMINI_LOCATION,CACHE_BUSTER=$(date +%s)"
if [ -n "${ELVISH_TTS_URL:-}" ]; then
    ENV_VARS="$ENV_VARS,ELVISH_TTS_URL=$ELVISH_TTS_URL"
    echo "-> Configured with TTS Service URL: $ELVISH_TTS_URL"
fi
if [ -n "${GEMINI_TRANSLATE_MODEL:-}" ]; then
    ENV_VARS="$ENV_VARS,GEMINI_TRANSLATE_MODEL=$GEMINI_TRANSLATE_MODEL"
    echo "-> Configured Gemini translate model: $GEMINI_TRANSLATE_MODEL"
fi

echo "Deploying with environment variables: FIREBASE_PROJECT_ID=$FIREBASE_PROJECT_ID, FIREBASE_DATABASE=$FIREBASE_DATABASE"
echo "Deploying..."

# Build and Deploy using Google Cloud Build (source-based deployment)
# - --service-account binds our dedicated, minimal runner SA
# - --memory 256Mi and --cpu 1 keep resource footprint very low and cost-efficient
# - --session-affinity ensures sticky routing to the same container for SSE sessions
gcloud run deploy "$SERVICE_NAME" \
    --source "$PROJECT_ROOT" \
    --region "$GCP_REGION" \
    --memory "256Mi" \
    --cpu "1" \
    --port "8080" \
    --service-account "$SERVICE_ACCOUNT_EMAIL" \
    --allow-unauthenticated \
    --session-affinity \
    --max-instances 1 \
    --set-env-vars "$ENV_VARS"

echo ""
echo "========================================="
echo " Deployment Complete! "
echo "========================================="
echo "Custom domain:  https://candir.mithlond.com"
echo "MCP endpoint:   https://candir.mithlond.com/sse"
echo "A2A endpoint:   https://candir.mithlond.com/a2a"
echo "AgentCard:      https://candir.mithlond.com/.well-known/agent-card.json"
_RAW_URL=$(gcloud run services describe "$SERVICE_NAME" --region "$GCP_REGION" \
    --format="value(status.url)" 2>/dev/null || echo "(run: gcloud run services describe $SERVICE_NAME --region $GCP_REGION)")
echo "Raw Cloud Run:  $_RAW_URL"
echo "========================================="
echo "Note: DNS CNAME candir.mithlond.com → ghs.googlehosted.com"
echo "  Domain mapping: gcloud run domain-mappings describe --domain candir.mithlond.com --region $GCP_REGION"
echo "========================================="
