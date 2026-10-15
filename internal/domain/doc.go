// Package domain defines the market data and order types that every adapter
// and feature shares. Build values with the New and Parse functions: they
// validate, and code that holds a value they returned does not check it again.
// The zero value of each struct is not a valid domain object.
//
// Prices and values are shopspring/decimal values, stored as NUMERIC in
// Postgres. Every Quote and Bar carries a Source and a Freshness.
package domain
