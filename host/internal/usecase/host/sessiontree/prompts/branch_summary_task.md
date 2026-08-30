<conversation>
{{.Conversation}}
</conversation>
{{if .HasAdditionalFocus}}
<additional_focus>
{{.AdditionalFocus}}
</additional_focus>
{{end}}
<task>
Summarize the source conversation. Apply the system rules and return only the approved applicable sections.
</task>
