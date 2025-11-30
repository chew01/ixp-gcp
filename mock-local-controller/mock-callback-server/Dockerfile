FROM golang:1.25-alpine AS build
WORKDIR /app
COPY . .
RUN go build -o callback .

FROM alpine
WORKDIR /app
COPY --from=build /app/callback .
EXPOSE 8080
CMD ["./callback"]
