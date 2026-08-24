# decimal

[![CI](https://github.com/joeychilson/decimal/actions/workflows/ci.yml/badge.svg)](https://github.com/joeychilson/decimal/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/joeychilson/decimal.svg)](https://pkg.go.dev/github.com/joeychilson/decimal)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

An arbitrary-precision finite decimal arithmetic library for Go.

Requires Go 1.27 or later.

## Installation

```text
go get github.com/joeychilson/decimal
```

## Package

- `decimal` — exact base-10 arithmetic, explicit rounding, conversions, formatting, encoding, and database integration.

## Example

Calculate an invoice total without converting monetary values to binary
floating point. Multiplication is exact, and the tax is explicitly rounded to
the invoice's two-decimal scale.

```go
package main

import (
	"fmt"
	"log"

	"github.com/joeychilson/decimal"
)

func main() {
	unitPrice := decimal.MustParse("19.99")
	quantity := decimal.FromInt(3)
	taxRate := decimal.MustParse("0.0825")

	subtotal, err := unitPrice.Mul(quantity)
	if err != nil {
		log.Fatal(err)
	}
	taxExact, err := subtotal.Mul(taxRate)
	if err != nil {
		log.Fatal(err)
	}
	tax, err := taxExact.Rescale(2, decimal.HalfEven)
	if err != nil {
		log.Fatal(err)
	}
	total := subtotal.Add(tax)

	fmt.Printf("subtotal: $%s\n", subtotal)
	fmt.Printf("tax:      $%s\n", tax)
	fmt.Printf("total:    $%s\n", total)

	// Output:
	// subtotal: $59.97
	// tax:      $4.95
	// total:    $64.92
}
```

## Development

```text
golangci-lint run ./...
go test ./...
go test -race ./...
go build ./...
```

## Release

Push a semantic version tag in the form `vMAJOR.MINOR.PATCH`, optionally with a
prerelease suffix, to create a GitHub Release with generated notes. The release
publishes the Go module source and does not include binary artifacts.

## License

[MIT](LICENSE)
