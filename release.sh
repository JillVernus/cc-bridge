#!/bin/bash
# Release script for cc-bridge
# Usage: ./release.sh [version]
# Example: ./release.sh v1.0.2

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get version from argument or VERSION file
if [ -n "$1" ]; then
    VERSION="$1"
else
    VERSION=$(cat VERSION)
fi

# Ensure version starts with 'v'
if [[ ! "$VERSION" =~ ^v ]]; then
    VERSION="v$VERSION"
fi

echo -e "${YELLOW}🚀 Releasing ${VERSION}${NC}"
echo ""

# Check for uncommitted changes
if [ -n "$(git status --porcelain)" ]; then
    echo -e "${RED}❌ Error: You have uncommitted changes${NC}"
    echo "Please commit or stash your changes first."
    git status --short
    exit 1
fi

# Check if tag already exists
if git rev-parse "$VERSION" >/dev/null 2>&1; then
    echo -e "${RED}❌ Error: Tag ${VERSION} already exists${NC}"
    echo "Use a different version or delete the existing tag."
    exit 1
fi

# Update VERSION file if needed
CURRENT_VERSION=$(cat VERSION)
if [ "$CURRENT_VERSION" != "$VERSION" ]; then
    echo -e "${YELLOW}📝 Updating VERSION file to ${VERSION}${NC}"
    echo "$VERSION" > VERSION
    git add VERSION
    git commit -m "chore: bump version to ${VERSION}"
fi

# Push commits
echo -e "${YELLOW}📤 Pushing commits to origin...${NC}"
git push origin main

# Create and push tag
echo -e "${YELLOW}🏷️  Creating tag ${VERSION}...${NC}"
git tag "$VERSION"

echo -e "${YELLOW}📤 Pushing tag to trigger Docker build...${NC}"
git push origin "$VERSION"

echo ""
echo -e "${GREEN}✅ Release ${VERSION} complete!${NC}"
echo ""
echo "GitHub Actions will now build and push the Docker image."
echo "Check progress at: https://github.com/JillVernus/cc-bridge/actions"
echo ""
echo "Once complete, the image will be available at:"
echo "  ghcr.io/jillvernus/cc-bridge:${VERSION}"
echo "  ghcr.io/jillvernus/cc-bridge:latest"
