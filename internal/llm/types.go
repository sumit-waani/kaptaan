package llm

// Message is a single turn in the conversation.
//
// NOTE: Content is intentionally NOT `omitempty`. DeepSeek's strict deserializer
// requires the `content` field to be present on every message — even on
// assistant messages whose only payload is `tool_calls` (where content is "").
// Omitting it triggers `messages[N]: missing field 'content'` 400 errors.
//
// ReasoningContent is the chain-of-thought field emitted by thinking models
// (deepseek-v4-pro). DeepSeek requires it to be echoed back verbatim on
// assistant messages in subsequent turns; dropping it causes a 400 error:
// "The reasoning_content in the thinking mode must be passed back to the API."
type Message struct {
        Role             string     `json:"role"`
        Content          string     `json:"content"`
        ReasoningContent string     `json:"reasoning_content,omitempty"`
        ToolCallID       string     `json:"tool_call_id,omitempty"`
        ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is a function call requested by the model.
type ToolCall struct {
        ID       string `json:"id"`
        Type     string `json:"type"`
        Function struct {
                Name      string `json:"name"`
                Arguments string `json:"arguments"`
        } `json:"function"`
}

// Response is the full API response.
type Response struct {
        Choices []Choice `json:"choices"`
        Usage   struct {
                PromptTokens     int `json:"prompt_tokens"`
                CompletionTokens int `json:"completion_tokens"`
                TotalTokens      int `json:"total_tokens"`
        } `json:"usage"`
        Error *struct {
                Message string `json:"message"`
        } `json:"error,omitempty"`
}

// Choice is one completion candidate.
type Choice struct {
        Message      Message `json:"message"`
        FinishReason string  `json:"finish_reason"`
}

// Tool defines a function the model can call.
type Tool struct {
        Type     string `json:"type"`
        Function struct {
                Name        string                 `json:"name"`
                Description string                 `json:"description"`
                Parameters  map[string]interface{} `json:"parameters"`
        } `json:"function"`
}

// UsageRecord is what gets persisted per call.
type UsageRecord struct {
        Provider         string
        Model            string
        PromptTokens     int
        CompletionTokens int
}
