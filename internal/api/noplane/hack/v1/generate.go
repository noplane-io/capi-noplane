//go:build generate

package v1

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config=./oapi-codegen.config.yaml ../../../../openapi/v1.yaml
