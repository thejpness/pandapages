package model

// AdminStoryOrchestrationRunSummary is lightweight immutable orchestration-run
// metadata for an admin source-version history. Complete retained evidence is
// deliberately available only from the single-run detail endpoint.
type AdminStoryOrchestrationRunSummary struct {
	ID              string `json:"id"`
	SourceVersionID string `json:"sourceVersionId"`
	SourceSHA256    string `json:"sourceSha256"`
	SemanticResult  string `json:"semanticResult"`
	CreatedAt       string `json:"createdAt"`
}

// AdminStoryOrchestrationRunsListResponse is a bounded newest-first list of
// orchestration-run summaries for one exact source version.
type AdminStoryOrchestrationRunsListResponse struct {
	Items []AdminStoryOrchestrationRunSummary `json:"items"`
}
