# Ticket: PHS-10 - Extension actions, user interaction, coordination, and provider authentication

## Status

The four problem definitions are agreed. Delivery is planned. PHS-10 is divided into the subphases below. Requirements, acceptance scenarios, technical solutions, and implementation order have not been defined for these subphases.

## Phase structure

| Subphase | Problem description |
| --- | --- |
| PHS-10.1 - Extension-defined slash commands | [Problem statement](01-extension-slash-commands/slash-commands_problem.md) |
| PHS-10.2 - Direct extension interaction with users | [Problem statement](02-extension-user-interaction/user-interaction_problem.md) |
| PHS-10.3 - Coordinated behavior of an extension set | [Problem statement](03-coordinated-extension-behavior/extension-coordination_problem.md) |
| PHS-10.4 - Provider authentication through Programmatic Control | [Problem statement](04-programmatic-provider-authentication/provider-authentication_problem.md) |

Each subphase has its own directory for its problem description and subsequent documents. This ticket indexes the four problems; their descriptions are maintained in the linked documents.

## Decisions

### Rejected problem: extension-initiated main-session control

No separate user problem has been established that requires extensions to initiate creation, resumption, forking, cloning, or navigation of the main active session. Providing that access to extensions is outside PHS-10.

The considered session-switching scenario is already served by built-in user commands. An extension command that repeats the same action adds no demonstrated user capability. Preparing context for a later session is also achievable by asking the agent to write a handoff document and then using that document in the next session.

The former command-initiated session-control scope and automated handoff scenario are therefore removed from this phase. This decision leaves existing user-invoked session operations, session-bound extension reads and writes, and extension state recovery unchanged. It does not establish a new problem or requirement for separate child sessions.

## Scope boundary

Only the four linked problems are part of this phase. The extension-coordination problem describes the shared user scenario and its participants; a coordination mechanism has not been selected.

Resource contributions, provider registration, TUI component contributions, and built-in child-agent or workflow semantics remain outside PHS-10.

## Open questions

None.

## References

- [Roadmap](../../../../../roadmap.md#phs-10-extension-actions-user-interaction-coordination-and-provider-authentication)
- [Delivery plan](../../delivery-plan.md)
- [Product requirements](../../prd.md)
- [Domain glossary](../../../../../terms.md)
