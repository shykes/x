# Generated by https://smithery.ai. See: https://smithery.ai/docs/config#dockerfile
# Multi-stage build: compile TypeScript and run MCP server

# Stage 1: build
FROM node:lts-alpine AS builder
WORKDIR /app/mcp-server

# Copy source and config
COPY mcp-server/package.json mcp-server/tsconfig.json ./
COPY mcp-server/index.ts ./

# Install dependencies including dev
RUN npm install
# Install TypeScript compiler for build
RUN npm install typescript --save-dev
# Compile TypeScript to dist
RUN npx tsc
# Remove dev dependencies
RUN npm prune --production

# Stage 2: runtime
FROM node:lts-alpine AS runtime
WORKDIR /app/mcp-server

# Copy compiled code and production dependencies
COPY --from=builder /app/mcp-server/dist ./dist
COPY --from=builder /app/mcp-server/node_modules ./node_modules
COPY mcp-server/package.json ./

# Start the MCP server
CMD ["node", "dist/index.js"]
