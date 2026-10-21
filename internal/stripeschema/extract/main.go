// Command extract trims a Stripe OpenAPI spec (github.com/stripe/openapi,
// openapi/spec3.json) to the object schemas stripeschema validates against.
//
//	go run ./internal/stripeschema/extract spec3.json internal/stripeschema/schemas.json.gz
package main

import (
	"compress/gzip"
	"encoding/json"
	"log"
	"os"
)

var keptKeywords = map[string]bool{
	"$ref": true, "additionalProperties": true, "anyOf": true, "allOf": true, "enum": true,
	"format": true, "items": true, "nullable": true, "oneOf": true, "required": true, "type": true,
}

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: extract <spec3.json> <schemas.json.gz>")
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		log.Fatal(err)
	}
	trimmed := map[string]any{}
	for name, schema := range spec.Components.Schemas {
		trimmed[name] = trim(schema)
	}
	out, err := os.Create(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	writer, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(writer).Encode(trimmed); err != nil {
		log.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		log.Fatal(err)
	}
}

func trim(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, field := range typed {
			switch {
			case key == "properties":
				properties := map[string]any{}
				for name, property := range field.(map[string]any) {
					properties[name] = trim(property)
				}
				out[key] = properties
			case keptKeywords[key]:
				out[key] = trim(field)
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = trim(item)
		}
		return out
	default:
		return value
	}
}
