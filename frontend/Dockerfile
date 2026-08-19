# Stage 1: Build the React application
FROM node:22-alpine AS build

WORKDIR /app

# Copy package files first for better layer caching
COPY package.json package-lock.json ./

# Install dependencies
RUN npm ci

# Copy source code
COPY . .

# Build the production bundle
RUN npm run build

# Stage 2: Serve with nginx on minimal alpine
FROM alpine:3.21 AS production

RUN apk add --no-cache nginx && \
    mkdir -p /usr/share/nginx/html && \
    rm -rf /var/cache/apk/*

# Copy custom nginx config
COPY nginx.conf /etc/nginx/http.d/default.conf

# Copy built assets from build stage
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]

# Stage 3: Development (with Vite dev server)
FROM node:22-alpine AS development

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .

EXPOSE 5173

CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
