module github.com/AltairaLabs/promptarena-deploy-vertex

go 1.25.1

require (
	github.com/AltairaLabs/PromptKit/runtime v1.3.2
	github.com/AltairaLabs/PromptKit/sdk v1.3.2
)

replace github.com/AltairaLabs/PromptKit/runtime => ../promptkit/runtime

replace github.com/AltairaLabs/PromptKit/pkg => ../promptkit/pkg

replace github.com/AltairaLabs/PromptKit/sdk => ../promptkit/sdk

replace github.com/AltairaLabs/PromptKit/server/a2a => ../promptkit/server/a2a
