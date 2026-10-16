FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /go-demo-api .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates
COPY --from=build /go-demo-api /go-demo-api
EXPOSE 8090
CMD ["/go-demo-api"]
