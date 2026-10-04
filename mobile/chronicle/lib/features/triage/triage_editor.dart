/// The per-item override sheet (CHRN-63): the Scribe's proposal, changed.
///
/// An override is not a patch, so the sheet holds every field the chosen
/// destination requires and [validateDraft] refuses the blanks the server would
/// refuse anyway. Returns the [Draft], or null if dismissed.
library;

import 'package:flutter/material.dart';

import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../../triage/triage_rows.dart';

Future<Draft?> showTriageEditor(
  BuildContext context, {
  required Draft initial,
  required String excerpt,
  required List<String> projectKeys,
}) =>
    showModalBottomSheet<Draft>(
      context: context,
      isScrollControlled: true,
      backgroundColor: chBase,
      builder: (_) => _Editor(initial: initial, excerpt: excerpt, projectKeys: projectKeys),
    );

class _Editor extends StatefulWidget {
  const _Editor({required this.initial, required this.excerpt, required this.projectKeys});

  final Draft initial;
  final String excerpt;
  final List<String> projectKeys;

  @override
  State<_Editor> createState() => _EditorState();
}

class _EditorState extends State<_Editor> {
  late Draft _d = widget.initial;
  late final _title = TextEditingController(text: _d.title);
  late final _text = TextEditingController(text: _d.text);
  late final _project = TextEditingController(text: _d.projectKey);
  late final _target = TextEditingController(text: _d.targetNote);
  late final _page = TextEditingController(text: _d.pagePath);
  String? _error;

  @override
  void dispose() {
    _title.dispose();
    _text.dispose();
    _project.dispose();
    _target.dispose();
    _page.dispose();
    super.dispose();
  }

  Draft _current() => _d.copyWith(
        title: _title.text,
        text: _text.text,
        projectKey: _project.text,
        targetNote: _target.text,
        pagePath: _page.text,
      );

  void _confirm() {
    final d = _current();
    final problem = validateDraft(d);
    if (problem != null) {
      setState(() => _error = problem);
      return;
    }
    Navigator.of(context).pop(d);
  }

  Widget _field(String label, TextEditingController c, String keyName, {int lines = 1}) => Padding(
        padding: const EdgeInsets.only(bottom: space2),
        child: TextField(
          key: ValueKey(keyName),
          controller: c,
          minLines: lines,
          maxLines: lines == 1 ? 1 : 8,
          decoration: InputDecoration(labelText: label, border: const OutlineInputBorder()),
        ),
      );

  @override
  Widget build(BuildContext context) {
    final dest = _d.destination;
    return Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.of(context).viewInsets.bottom),
      child: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(space3),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('OVERRIDE THE PROPOSAL', style: microLabel(color: chSignal, size: sizeSm)),
              const SizedBox(height: space1),
              Text(widget.excerpt, maxLines: 3, overflow: TextOverflow.ellipsis, style: monoMeta()),
              const SizedBox(height: space2),
              Wrap(
                spacing: space1,
                children: [
                  for (final o in const ['NOTE', 'TICKET', 'DISCUSSION', 'DISCARD'])
                    ChoiceChip(
                      key: ValueKey('dest-$o'),
                      label: Text(destinationTag(o)),
                      selected: dest == o,
                      onSelected: (_) => setState(() => _d = _current().copyWith(destination: o)),
                    ),
                ],
              ),
              const SizedBox(height: space2),
              if (dest != 'DISCARD') ...[
                _field('Title', _title, 'editor-title'),
                if (dest == 'TICKET') ...[
                  _field('Project key', _project, 'editor-project'),
                  if (widget.projectKeys.isNotEmpty)
                    Wrap(
                      spacing: space1,
                      children: [
                        for (final k in widget.projectKeys)
                          ActionChip(label: Text(k), onPressed: () => setState(() => _project.text = k)),
                      ],
                    ),
                  DropdownButtonFormField<String>(
                    key: const ValueKey('editor-type'),
                    initialValue: ticketTypes.contains(_d.ticketType) ? _d.ticketType : 'task',
                    items: [for (final t in ticketTypes) DropdownMenuItem(value: t, child: Text(t))],
                    onChanged: (v) => setState(() => _d = _current().copyWith(ticketType: v)),
                    decoration: const InputDecoration(labelText: 'Type', border: OutlineInputBorder()),
                  ),
                  const SizedBox(height: space2),
                ],
                if (dest == 'NOTE') ...[
                  Wrap(
                    spacing: space1,
                    children: [
                      for (final v in const ['create', 'append', 'supersede', 'relate'])
                        ChoiceChip(
                          key: ValueKey('verb-$v'),
                          label: Text(v.toUpperCase()),
                          selected: _d.verb == v,
                          onSelected: (_) => setState(() => _d = _current().copyWith(verb: v)),
                        ),
                    ],
                  ),
                  const SizedBox(height: space2),
                  if (_d.verb != 'create') _field('Note reference', _target, 'editor-target'),
                  _field('Page path', _page, 'editor-page'),
                ],
                _field(textLabel(dest), _text, 'editor-text', lines: 4),
              ] else
                Text(
                  'A discard is final once it is sent. You get 10 minutes to undo it on the row.',
                  style: monoMeta(),
                ),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: space2),
                  child: Text(_error!, key: const ValueKey('editor-error'), style: const TextStyle(color: chText)),
                ),
              SizedBox(
                width: double.infinity,
                child: FilledButton(
                  key: const ValueKey('editor-confirm'),
                  onPressed: _confirm,
                  style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(minTapTarget)),
                  child: Text(dest == 'DISCARD' ? 'DISCARD' : 'CONFIRM'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
