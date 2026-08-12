package vertex

// configSchema is the JSON Schema (draft-07) for the vertex provider config.
const configSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["project", "location"],
  "properties": {
    "project": { "type": "string" },
    "location": { "type": "string" }
  }
}`
