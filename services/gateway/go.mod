module github.com/icofcucam/naditos/services/gateway

go 1.22

require (
	github.com/google/uuid v1.6.0
	github.com/icofcucam/naditos/packages/go-common v0.0.0
	golang.org/x/time v0.5.0
)

require (
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	golang.org/x/crypto v0.24.0 // indirect
)

replace github.com/icofcucam/naditos/packages/go-common => ../../packages/go-common
