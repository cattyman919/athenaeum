FROM node:26-alpine AS build
WORKDIR /src
# Node 25+ no longer ships corepack, so install the pinned pnpm directly.
RUN npm install -g pnpm@9.15.9
# next.config.ts bakes the /api/* rewrite target at build time.
ARG API_BASE_URL=http://api:8080
ENV API_BASE_URL=$API_BASE_URL
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM node:26-alpine
WORKDIR /src
ENV NODE_ENV=production
COPY --from=build /src/package.json ./package.json
COPY --from=build /src/.next ./.next
COPY --from=build /src/public ./public
COPY --from=build /src/node_modules ./node_modules
EXPOSE 3000
ENTRYPOINT ["node_modules/.bin/next", "start"]
