# Standard TUI Commands and Key Defaults

This supplement owns the concrete command names and initial keybinding baseline for the standard TUI. Capability requirements and ownership boundaries are defined in `docs/specs/features/initial/prd.md` and `docs/specs/features/initial/standard-tui.md`.

The keybinding baseline was copied from Pi instead of being designed again. Glyph owns the recorded values; later Pi changes do not update them automatically. A row applies only when Glyph independently supports the equivalent action and does not add that action to product scope. Pi reference action identifiers identify source semantics and are not Glyph API names.

## Command Names

| Capability | Command |
|---|---|
| Select a model | `/model` |
| Reload the Glyph environment | `/reload` |
| Compact model context | `/compact` |
| Start a session | `/new` |
| Resume a session | `/resume` |
| Navigate the session tree | `/tree` |
| Fork a session | `/fork` |
| Clone a session | `/clone` |
| Name a session | `/name` |
| Show session information | `/session` |
| Show active commands and key bindings | `/hotkeys` |

## Keybinding Baseline

The following values are the complete Pi baseline for macOS and Linux. Platform-specific Windows alternatives are excluded because Windows support is outside Glyph scope. `None` means that the action has no default key combination.

### Editor Cursor Movement

| Pi reference action | Default |
|---|---|
| `tui.editor.cursorUp` | `up` |
| `tui.editor.cursorDown` | `down` |
| `tui.editor.historyPrevious` | None |
| `tui.editor.historyNext` | None |
| `tui.editor.cursorLeft` | `left`, `ctrl+b` |
| `tui.editor.cursorRight` | `right`, `ctrl+f` |
| `tui.editor.cursorWordLeft` | `alt+left`, `ctrl+left`, `alt+b` |
| `tui.editor.cursorWordRight` | `alt+right`, `ctrl+right`, `alt+f` |
| `tui.editor.cursorLineStart` | `home`, `ctrl+home`, `ctrl+a` |
| `tui.editor.cursorLineEnd` | `end`, `ctrl+end`, `ctrl+e` |
| `tui.editor.jumpForward` | `ctrl+]` |
| `tui.editor.jumpBackward` | `ctrl+alt+]` |
| `tui.editor.pageUp` | `pageUp`, `ctrl+pageUp` |
| `tui.editor.pageDown` | `pageDown`, `ctrl+pageDown` |

### Editor Deletion

| Pi reference action | Default |
|---|---|
| `tui.editor.deleteCharBackward` | `backspace` |
| `tui.editor.deleteCharForward` | `delete`, `ctrl+d` |
| `tui.editor.deleteWordBackward` | `ctrl+w`, `alt+backspace` |
| `tui.editor.deleteWordForward` | `alt+d`, `alt+delete` |
| `tui.editor.deleteToLineStart` | `ctrl+u` |
| `tui.editor.deleteToLineEnd` | `ctrl+k` |

### Input

| Pi reference action | Default |
|---|---|
| `tui.input.newLine` | `shift+enter`, `ctrl+j` |
| `tui.input.submit` | `enter` |
| `tui.input.tab` | `tab` |

### Kill Ring

| Pi reference action | Default |
|---|---|
| `tui.editor.yank` | `ctrl+y` |
| `tui.editor.yankPop` | `alt+y` |
| `tui.editor.undo` | `ctrl+-` |

### Clipboard and Selection

| Pi reference action | Default |
|---|---|
| `tui.input.copy` | `ctrl+c` |
| `tui.select.up` | `up` |
| `tui.select.down` | `down` |
| `tui.select.pageUp` | `pageUp` |
| `tui.select.pageDown` | `pageDown` |
| `tui.select.confirm` | `enter` |
| `tui.select.cancel` | `escape`, `ctrl+c` |

### Transcript Viewport

| Pi reference action | Default |
|---|---|
| `tui.altScreen.pageUp` | `pageUp` |
| `tui.altScreen.pageDown` | `pageDown` |
| `tui.altScreen.halfPageUp` | None |
| `tui.altScreen.halfPageDown` | None |
| `tui.altScreen.lineUp` | None |
| `tui.altScreen.lineDown` | None |
| `tui.altScreen.previousPrompt` | `ctrl+shift+up` |
| `tui.altScreen.nextPrompt` | `ctrl+shift+down` |
| `tui.altScreen.search` | `ctrl+shift+f` |
| `tui.altScreen.searchNext` | `enter`, `ctrl+g` |
| `tui.altScreen.searchPrevious` | `shift+enter`, `ctrl+shift+g` |
| `tui.altScreen.searchClose` | `escape` |
| `tui.altScreen.top` | `home` |
| `tui.altScreen.bottom` | `end` |

In the standard TUI transcript, unmodified `home`, `end`, `pageUp`, and `pageDown` use the viewport actions above. Their `ctrl` variants retain editor movement. User configuration can replace either binding.

### Application

| Pi reference action | Default |
|---|---|
| `app.interrupt` | `escape` |
| `app.clear` | `ctrl+c` |
| `app.exit` | `ctrl+d` |
| `app.suspend` | `ctrl+z` |
| `app.editor.external` | `ctrl+g` |
| `app.clipboard.pasteImage` | `ctrl+v` |

### Sessions

| Pi reference action | Default |
|---|---|
| `app.session.new` | None |
| `app.session.tree` | None |
| `app.session.fork` | None |
| `app.session.resume` | None |
| `app.session.togglePath` | `ctrl+p` |
| `app.session.toggleSort` | `ctrl+s` |
| `app.session.toggleNamedFilter` | `ctrl+n` |
| `app.session.rename` | `ctrl+r` |
| `app.session.delete` | `ctrl+d` |
| `app.session.deleteNoninvasive` | `ctrl+backspace` |

### Models and Reasoning

| Pi reference action | Default |
|---|---|
| `app.model.select` | `ctrl+l` |
| `app.model.cycleForward` | `ctrl+p` |
| `app.model.cycleBackward` | `shift+ctrl+p` |
| `app.thinking.cycle` | `shift+tab` |
| `app.thinking.toggle` | `ctrl+t` |

### Display and Message Queue

| Pi reference action | Default |
|---|---|
| `app.tools.expand` | `ctrl+o` |
| `app.message.copy` | `ctrl+x` |
| `app.message.followUp` | `alt+enter` |
| `app.message.dequeue` | `alt+up` |

### Tree Navigation

| Pi reference action | Default |
|---|---|
| `app.tree.foldOrUp` | `ctrl+left`, `alt+left` |
| `app.tree.unfoldOrDown` | `ctrl+right`, `alt+right` |
| `app.tree.editLabel` | `shift+l` |
| `app.tree.toggleLabelTimestamp` | `shift+t` |
| `app.tree.filter.default` | `ctrl+d` |
| `app.tree.filter.noTools` | `ctrl+t` |
| `app.tree.filter.userOnly` | `ctrl+u` |
| `app.tree.filter.labeledOnly` | `ctrl+l` |
| `app.tree.filter.all` | `ctrl+a` |
| `app.tree.filter.cycleForward` | `ctrl+o` |
| `app.tree.filter.cycleBackward` | `shift+ctrl+o` |

### Scoped Models Selector

| Pi reference action | Default |
|---|---|
| `app.models.save` | `ctrl+s` |
| `app.models.enableAll` | `ctrl+a` |
| `app.models.clearAll` | `ctrl+x` |
| `app.models.toggleProvider` | `ctrl+p` |
| `app.models.reorderUp` | `alt+up` |
| `app.models.reorderDown` | `alt+down` |

## Open Questions

None.

## References

- `docs/specs/features/initial/prd.md`
- `docs/specs/features/initial/standard-tui.md`
- `https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/usage.md`
- `https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/keybindings.md`
