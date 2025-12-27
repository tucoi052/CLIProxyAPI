#!/bin/bash
# Test script for Antigravity Quota UI

echo "🧪 Testing Antigravity Quota UI"
echo "=================================="
echo ""

BASE_URL="http://localhost:8317"
HTML_URL="${BASE_URL}/antigravity-quota.html"
API_URL="${BASE_URL}/v1/antigravity-quota"

# Test 1: HTML File
echo "1. Testing HTML File..."
html_status=$(curl -s -o /dev/null -w "%{http_code}" "$HTML_URL")
if [ "$html_status" = "200" ]; then
    echo "   ✅ HTML file accessible (HTTP $html_status)"
else
    echo "   ❌ HTML file failed (HTTP $html_status)"
    exit 1
fi

# Test 2: API Endpoint
echo ""
echo "2. Testing API Endpoint..."
api_status=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL")
if [ "$api_status" = "200" ]; then
    echo "   ✅ API endpoint accessible (HTTP $api_status)"
else
    echo "   ❌ API endpoint failed (HTTP $api_status)"
    exit 1
fi

# Test 3: HTML Content
echo ""
echo "3. Checking HTML Content..."
if curl -s "$HTML_URL" | grep -q "Antigravity Quota Monitor"; then
    echo "   ✅ Title found"
else
    echo "   ❌ Title not found"
fi

if curl -s "$HTML_URL" | grep -q "API_URL"; then
    echo "   ✅ JavaScript found"
else
    echo "   ❌ JavaScript not found"
fi

if curl -s "$HTML_URL" | grep -q "id=\"refreshBtn\""; then
    echo "   ✅ UI elements found"
else
    echo "   ❌ UI elements missing"
fi

# Test 4: API Response
echo ""
echo "4. Testing API Response..."
api_response=$(curl -s "$API_URL")
total_accounts=$(echo "$api_response" | jq -r '.total_accounts // 0')
active_accounts=$(echo "$api_response" | jq -r '.active_accounts // 0')

if [ "$total_accounts" -gt 0 ]; then
    echo "   ✅ API returns data"
    echo "   📊 Total Accounts: $total_accounts"
    echo "   📊 Active Accounts: $active_accounts"
else
    echo "   ⚠️  API returns no accounts"
fi

# Test 5: Check for required elements
echo ""
echo "5. Verifying Required Elements..."
elements=("refreshBtn" "statusBadge" "statTotal" "statActive" "content")
for element in "${elements[@]}"; do
    if curl -s "$HTML_URL" | grep -q "id=\"$element\""; then
        echo "   ✅ Element '$element' found"
    else
        echo "   ❌ Element '$element' missing"
    fi
done

# Test 6: Sample data check
echo ""
echo "6. Sample Data Check..."
sample_email=$(echo "$api_response" | jq -r '.accounts[0].email // "none"')
sample_models=$(echo "$api_response" | jq -r '.accounts[0].model_quotas | length // 0')

if [ "$sample_email" != "none" ] && [ "$sample_models" -gt 0 ]; then
    echo "   ✅ Sample account: $sample_email"
    echo "   ✅ Models count: $sample_models"
else
    echo "   ⚠️  No sample data available"
fi

echo ""
echo "=================================="
echo "✅ UI Test Complete!"
echo ""
echo "🌐 Open in browser: $HTML_URL"
echo ""

