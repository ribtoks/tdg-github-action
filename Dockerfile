FROM golang:1.22-alpine as builder

WORKDIR /app
COPY . /app

ENV GOFLAGS="-mod=vendor"

# Statically compile our app for use in the final container
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -v -o app .

FROM alpine:3.19

COPY --from=builder /app/app /app

RUN apk update && apk --no-cache add git

ENTRYPOINT ["/app"]
