#!/bin/bash
# Script to test Antigravity Quota API

set -e

echo "=== Testing Antigravity Quota API ==="
echo ""

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker daemon is not running. Please start Docker Desktop first."
    exit 1
fi

echo "✅ Docker is running"
echo ""

# Build the Docker image
echo "📦 Building Docker image..."
docker compose build --build-arg VERSION=dev --build-arg COMMIT=test --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

echo ""
echo "🚀 Starting container..."
docker compose up -d

echo ""
echo "⏳ Waiting for server to start..."
sleep 5

echo ""
echo "🧪 Testing API endpoint..."
echo ""

# Test the quota endpoint
BASE_URL="http://localhost:8317"
ENDPOINT="${BASE_URL}/v1/antigravity-quota"

echo "GET ${ENDPOINT}"
echo ""

response=$(curl -s -w "\nHTTP_STATUS:%{http_code}" "${ENDPOINT}" || echo "HTTP_STATUS:000")

http_status=$(echo "$response" | grep "HTTP_STATUS:" | cut -d: -f2)
body=$(echo "$response" | sed '/HTTP_STATUS:/d')

echo "HTTP Status: ${http_status}"
echo ""
echo "Response:"
echo "$body" | jq . 2>/dev/null || echo "$body"

echo ""
echo ""

# Check logs for quota-related messages
echo "📋 Recent quota-related logs:"
docker compose logs --tail=50 | grep -i "antigravity.*quota" || echo "No quota logs found"

echo ""
echo ""
echo "=== Test Complete ==="
echo ""
echo "To view full logs: docker compose logs -f"
echo "To stop: docker compose down"

