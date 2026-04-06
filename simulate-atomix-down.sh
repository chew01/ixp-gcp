#!/bin/bash

##############################################################################
# Demo: Atomix Down Scenario → Telegram Alert
# 
# This script demonstrates:
# 1. Scale down Atomix consensus-store
# 2. Generate errors that trigger the alert
# 3. Alert fires in Telegram
#
# Usage: ./simulate-atomix-down.sh
##############################################################################

set -e

API_HOST="localhost:8080"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}════════════════════════════════════════${NC}"
echo -e "${BLUE}Demo: Consensus Store Down → Alert${NC}"
echo -e "${BLUE}════════════════════════════════════════${NC}"
echo ""

# Scale down Atomix
echo -e "${YELLOW}Scaling down Consensus Store to 0 replicas...${NC}"
kubectl scale statefulset consensus-store --replicas=0 2>/dev/null || true
sleep 2
echo -e "${RED}❌ Consensus Store is DOWN${NC}"
echo ""

# Generate errors
echo -e "${YELLOW}Generating API errors to trigger alert...${NC}"
for i in {1..5}; do
  curl -s --max-time 5 "http://${API_HOST}/credits" -H "X-Customer-ID: customer-$i" 2>&1 > /dev/null &
done
wait
echo -e "${RED}✓ 5 error requests sent${NC}"
echo ""

# Wait for alert
echo -e "${YELLOW}Waiting for alert to fire in Telegram (30 seconds)...${NC}"
echo -e "${BLUE}Watch your Telegram for the alert!${NC}"
sleep 30

echo ""
echo -e "${BLUE}════════════════════════════════════════${NC}"
echo -e "${GREEN}Done! Check Telegram for alert.${NC}"
echo -e "${BLUE}════════════════════════════════════════${NC}"
echo ""


