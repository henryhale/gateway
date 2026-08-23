package domain

import gw "github.com/henryhale/gateway"

// OperationRandomQuote is the single operation this example gateway supports.
const OperationRandomQuote gw.Operation = "quote.random"

// QuoteRequest carries no application input; every provider is queried for
// one random quote with no parameters.
type QuoteRequest struct{}

// Quote is the standard response returned regardless of which provider
// served the request.
type Quote struct {
	Text   string `json:"text"`
	Author string `json:"author"`
	Source string `json:"source"`
}
