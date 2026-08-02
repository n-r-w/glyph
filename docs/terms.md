# Domain Glossary

- `Glyph`: The project name for the independent Go agent platform being defined.
- `Pi`: An external terminal coding agent platform used as a conceptual reference for Glyph.
- `pi-agent-suite`: The existing set of Pi extensions maintained by the Glyph project owner. Its capabilities provide reference scenarios for Glyph.
- `language model`: A model that receives context and produces text or tool requests.
- `agent`: A software system that uses a language model and available actions to fulfill a user request.
- `agent platform`: A reusable software foundation for creating and running different agents.
- `independent agent platform`: An agent platform that can be developed and released without using or changing the agent core of another platform.
- `agent core`: The required part of an agent platform that provides runtime behavior shared by its agents.
- `agent loop`: The repeated sequence of requesting a model response, executing model-requested actions, and returning their results to the model until the run completes or is stopped.
- `coding agent`: An agent intended to work with source code and related software development tasks.
- `tool`: A typed operation that an agent exposes to a model by name.
- `extension`: A component that adds or changes platform behavior through extension contracts without modifying the agent core source code.
- `extension contract`: A documented interface, data type, event, or registration point through which an extension interacts with the platform.
- `context`: The information sent to a model to produce its next response or tool request.
- `session`: A related sequence of user requests, model responses, tool calls, and agent state.
- `model provider`: A local or remote system through which an agent accesses a language model.
- `terminal user interface`: An interactive agent interface presented inside a terminal.
- `reference scenario`: Behavior from an existing system that is used to evaluate Glyph requirements or extension contracts without requiring source compatibility with that system.
