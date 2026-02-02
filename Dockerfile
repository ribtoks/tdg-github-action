FROM golang:1.25-alpine as builder

WORKDIR /app
COPY . /app
ARG GIT_COMMIT=unknown

ENV GOFLAGS="-mod=vendor"

# Statically compile our app for use in the final container
RUN CGO_ENABLED=0 go build -ldflags="-w -s -X main.GitCommit=${GIT_COMMIT}" -v -o app .

FROM alpine:3.23

COPY --from=builder /app/app /app

RUN apk update && apk --no-cache add git

ENTRYPOINT ["/app"]
