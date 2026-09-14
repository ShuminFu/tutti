package agentruntime

import "context"

// Local references are presentation objects, not provider attachments. All
// runtimes receive their prepared paths as ordinary text, including directories.
func projectRuntimePromptContent(content []PromptContentBlock) []PromptContentBlock {
	projected := projectRuntimeConnectorPromptContent(content)
	for index, block := range projected {
		if block.Type == "file" {
			projected[index] = PromptContentBlock{Type: "text", Text: block.Path}
		}
	}
	return projected
}

type submittedPromptContextKey struct{}

type submittedPrompt struct {
	content       []PromptContentBlock
	displayPrompt string
}

// Provider adapters also emit user-message receipts. Keep their presentation
// anchored to this submission, rather than persisting the wire projection.
func withSubmittedPrompt(ctx context.Context, content []PromptContentBlock, displayPrompt string) context.Context {
	return context.WithValue(ctx, submittedPromptContextKey{}, submittedPrompt{
		content:       append([]PromptContentBlock(nil), content...),
		displayPrompt: displayPrompt,
	})
}

func submittedPromptFromContext(ctx context.Context) (submittedPrompt, bool) {
	if ctx == nil {
		return submittedPrompt{}, false
	}
	prompt, ok := ctx.Value(submittedPromptContextKey{}).(submittedPrompt)
	return prompt, ok
}
