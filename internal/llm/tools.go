package llm

import "encoding/json"

type ToolDef struct {
	Spec     ToolSpec
	MinFlags string
	Kind     string // "help" (safe) or "ops" (kick/ban/topic)
	Run      func(ctx ToolCtx, args map[string]any) (string, error)
}

type ToolCtx struct {
	Handle  string
	Channel string
	Nick    string
}

func ParseArgs(raw string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return map[string]any{}
	}
	return m
}

func Str(args map[string]any, key string) string {
	if v, ok := args[key]; ok && v != nil {
		return toStr(v)
	}
	return ""
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func Schema(props map[string]any, required []string) map[string]any {
	s := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func Prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}
