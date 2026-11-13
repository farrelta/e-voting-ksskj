FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /server .
RUN mkdir /ballot-assets && \
    GOOS=js GOARCH=wasm go build -o /ballot-assets/ballot.wasm ./cmd/anonymous-ballot-wasm && \
    cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" /ballot-assets/

FROM gcr.io/distroless/static
COPY --from=build /server /server
COPY --from=build /ballot-assets /ballot-assets
CMD ["/server"]
