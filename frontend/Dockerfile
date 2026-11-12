# Browser encryption module: the same Go code the server uses, compiled to
# WebAssembly. Needs the "backend" build context (see docker-compose.yaml).
FROM golang:1.24 AS client-crypto
WORKDIR /src
COPY --from=backend go.mod go.sum ./
RUN go mod download
COPY --from=backend cryptoutil ./cryptoutil
COPY --from=backend cmd ./cmd
RUN GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o /out/clientcrypto.wasm ./cmd/clientcrypto \
    && cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" /out/wasm_exec.js

FROM node:22-alpine AS dependencies
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:22-alpine AS build
WORKDIR /app
ARG BACKEND_URL=http://localhost:3001
ENV BACKEND_URL=$BACKEND_URL
COPY --from=dependencies /app/node_modules ./node_modules
COPY . .
COPY --from=client-crypto /out ./public/crypto
RUN npm run build

FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/package.json ./package.json
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/.next ./.next
COPY --from=build /app/public ./public
COPY --from=build /app/next.config.ts ./next.config.ts
EXPOSE 3000
CMD ["npm", "run", "start"]
