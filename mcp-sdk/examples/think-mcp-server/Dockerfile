FROM node:18-alpine AS builder
WORKDIR /app

# Copy package files
COPY package*.json tsconfig.json ./

# Install dependencies with caching
RUN --mount=type=cache,target=/root/.npm npm install

# Copy source code
COPY . .

# Build the application
RUN npm run build

FROM node:18-alpine AS release
WORKDIR /app

# Copy only the necessary files from builder
COPY --from=builder /app/dist /app/dist
COPY --from=builder /app/package*.json ./

# Set production environment
ENV NODE_ENV=production

# Install only production dependencies
RUN npm ci --ignore-scripts --omit=dev

# Set executable permissions
RUN chmod +x dist/server.js

# Set the user to non-root
USER node

# Use ENTRYPOINT instead of CMD for better compatibility
ENTRYPOINT ["node", "dist/server.js"]