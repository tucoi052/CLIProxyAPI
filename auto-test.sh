#!/bin/bash
# Auto build and test script for Antigravity Quota

set -e

echo "🚀 Auto Build & Test Script for Antigravity Quota"
echo "=================================================="
echo ""

# Function to wait for Docker
wait_for_docker() {
    echo "⏳ Waiting for Docker to be ready..."
    local max_attempts=30
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        if docker info > /dev/null 2>&1; then
            echo "✅ Docker is ready!"
            return 0
        fi
        attempt=$((attempt + 1))
        echo "   Attempt $attempt/$max_attempts... (starting Docker Desktop?)"
        sleep 2
    done
    
    echo "❌ Docker is not available after $max_attempts attempts"
    echo "   Please start Docker Desktop and run this script again"
    return 1
}

# Check Docker
if ! docker info > /dev/null 2>&1; then
    echo "⚠️  Docker daemon is not running"
    wait_for_docker || exit 1
fi

echo ""
echo "📦 Step 1: Stopping existing containers..."
docker compose down 2>/dev/null || true

echo ""
echo "📦 Step 2: Building Docker image with latest changes..."
docker compose build \
    --build-arg VERSION=dev \
    --build-arg COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "local") \
    --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

echo ""
echo "🚀 Step 3: Starting container..."
docker compose up -d

echo ""
echo "⏳ Step 4: Waiting for server to be ready..."
sleep 8

# Check if server is responding
max_attempts=10
attempt=0
while [ $attempt -lt $max_attempts ]; do
    if curl -s http://localhost:8317/v1/models > /dev/null 2>&1; then
        echo "✅ Server is ready!"
        break
    fi
    attempt=$((attempt + 1))
    echo "   Waiting... ($attempt/$max_attempts)"
    sleep 2
done

if [ $attempt -eq $max_attempts ]; then
    echo "⚠️  Server might not be ready, but continuing..."
fi

echo ""
echo "🧪 Step 5: Testing Antigravity Quota API..."
echo "=========================================="
echo ""

ENDPOINT="http://localhost:8317/v1/antigravity-quota"
echo "GET $ENDPOINT"
echo ""

# Make request and capture response
response=$(curl -s -w "\n\nHTTP_STATUS:%{http_code}\nTIME:%{time_total}s" "$ENDPOINT" 2>&1)

# Extract parts
http_status=$(echo "$response" | grep "HTTP_STATUS:" | cut -d: -f2)
time_total=$(echo "$response" | grep "TIME:" | cut -d: -f2)
body=$(echo "$response" | sed '/HTTP_STATUS:/d' | sed '/TIME:/d')

echo "📊 Response Status: $http_status"
echo "⏱️  Response Time: ${time_total}s"
echo ""

if [ "$http_status" = "200" ]; then
    echo "✅ SUCCESS! API returned 200 OK"
    echo ""
    echo "📄 Response Body:"
    echo "$body" | jq . 2>/dev/null || echo "$body"
    
    # Extract some stats
    total=$(echo "$body" | jq -r '.total_accounts // 0' 2>/dev/null || echo "0")
    active=$(echo "$body" | jq -r '.active_accounts // 0' 2>/dev/null || echo "0")
    
    echo ""
    echo "📈 Summary:"
    echo "   Total Accounts: $total"
    echo "   Active Accounts: $active"
else
    echo "❌ FAILED! API returned $http_status"
    echo ""
    echo "📄 Response Body:"
    echo "$body"
fi

echo ""
echo "📋 Step 6: Checking logs for quota-related messages..."
echo "=================================================="
docker compose logs --tail=100 | grep -i "antigravity.*quota" | tail -20 || echo "No quota logs found"

echo ""
echo "📋 Recent error logs (if any):"
docker compose logs --tail=50 | grep -i "error.*quota" | tail -10 || echo "No errors found"

echo ""
echo "=================================================="
echo "✅ Test Complete!"
echo ""
echo "💡 Useful commands:"
echo "   View logs:     docker compose logs -f"
echo "   Stop server:   docker compose down"
echo "   Restart:       docker compose restart"
echo ""

