Inspect one model profile by id. See its purpose bindings, launcher model fields and named execution tiers. Edit it through the model-profiles HTTP API; saving does not apply purpose or launcher settings.

```json
{
  "type": "object",
  "properties": {
    "id": {
      "type": "string",
      "description": "Profile id from model_profiles_list"
    }
  },
  "additionalProperties": false,
  "required": [
    "id"
  ]
}
```
