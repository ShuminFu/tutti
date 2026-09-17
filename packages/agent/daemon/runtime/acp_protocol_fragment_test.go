package agentruntime

import "testing"

func TestIsAssistantProtocolFragment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "empty", text: "", want: false},
		{name: "normal answer", text: "已按工单要求完成修改。", want: false},
		{name: "comparison is not a tag", text: "use a < b when sorting", want: false},
		{name: "html mention is not DSML", text: "Wrap the label in a <div> please.", want: false},
		{name: "dsml leak", text: "<write_stdin\">\n</parameter>", want: true},
		{name: "parameter only", text: "<parameter name=\"cmd\">pwd</parameter>", want: true},
		{name: "unclosed tag", text: "<write_stdin", want: true},
		{name: "dsml plus real answer kept", text: "<parameter>x</parameter>\n已经检查完仓库里的相关文件。", want: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isAssistantProtocolFragment(test.text); got != test.want {
				t.Fatalf("isAssistantProtocolFragment(%q) = %v, want %v", test.text, got, test.want)
			}
		})
	}
}

func TestApplyAssistantFinalTextDropsDSMLFragment(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()
	normalizer.ApplyAssistantFinalText("<write_stdin\">\n</parameter>")
	events := normalizer.Finish(session, "turn-1", messageStreamStateCompleted)
	if content, ok := assertCompletedAssistantContent(t, events); ok {
		t.Fatalf("assistant answer = %q, want none for a DSML fragment", content)
	}
}
