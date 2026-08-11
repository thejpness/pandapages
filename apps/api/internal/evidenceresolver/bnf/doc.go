// Package bnf retrieves bounded structured work observations from the
// Bibliothèque nationale de France data.bnf.fr SPARQL endpoint. BnF documents
// bnf-onto:firstYear as a work's first-publication year; the adapter never
// substitutes an edition, manifestation, or digitisation date.
//
// The adapter makes one HTTPS request to data.bnf.fr/sparql, with a fixed
// query shape, a 15-second timeout, no proxy, no redirects, a 512 KiB response
// limit, and no pagination. It supplies bibliographic facts only. Panda Pages'
// resolver centrally classifies the source as authoritative for this one fact.
package bnf
