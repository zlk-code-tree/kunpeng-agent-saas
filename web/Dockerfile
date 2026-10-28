FROM node:24-alpine AS builder
WORKDIR /src/web
RUN corepack enable
COPY web/ .
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @vben/web-ele build

FROM nginx:1.29-alpine
COPY --from=builder /src/web/apps/web-ele/dist /usr/share/nginx/html
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
