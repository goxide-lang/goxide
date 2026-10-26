import { useLayoutEffect, useRef, useId } from "preact/hooks";
import * as monaco from "monaco-editor/editor/editor.api";
import "monaco-editor/editor/contrib/find/browser/findController";
import "monaco-editor/editor/contrib/suggest/browser/suggestController";
import "monaco-editor/editor/contrib/clipboard/browser/clipboard";
import "monaco-editor/editor/contrib/comment/browser/comment";
import "monaco-editor/editor/contrib/bracketMatching/browser/bracketMatching";
import "monaco-editor/editor/contrib/multicursor/browser/multicursor";
import "monaco-editor/editor/contrib/toggleTabFocusMode/browser/toggleTabFocusMode";
import "monaco-editor/editor/contrib/gotoError/browser/gotoError";
import "monaco-editor/languages/definitions/go/register";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
self.MonacoEnvironment = { getWorker: () => new EditorWorker() };

export default function MonacoPane({
  files,
  activePath,
  readonly,
  markers = [],
  position,
  onChange,
  onCompile,
  label,
  onReady,
}) {
  const host = useRef(null),
    editor = useRef(null),
    models = useRef(new Map()),
    views = useRef(new Map()),
    switching = useRef(false),
    callbacks = useRef({ onChange, onCompile });
  const scope = useId();
  callbacks.current = { onChange, onCompile };
  useLayoutEffect(() => {
    const instance = monaco.editor.create(host.current, {
      model: null,
      automaticLayout: true,
      minimap: { enabled: false },
      fontSize: 16,
      lineHeight: 26,
      padding: { top: 14, bottom: 14 },
      lineNumbersMinChars: 3,
      scrollBeyondLastLine: false,
      wordBasedSuggestions: "currentDocument",
      readOnly: readonly,
      ariaLabel: label,
    });
    editor.current = instance;
    onReady?.();
    const change = instance.onDidChangeModelContent(() => {
      if (
        !switching.current &&
        !instance.getOption(monaco.editor.EditorOption.readOnly)
      )
        callbacks.current.onChange?.(
          instance.getModel().uri.path.slice(1),
          instance.getValue(),
        );
    });
    instance.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, () =>
      callbacks.current.onCompile?.(),
    );
    return () => {
      change.dispose();
      instance.dispose();
      for (const model of models.current.values()) model.dispose();
      models.current.clear();
      views.current.clear();
    };
  }, []);
  useLayoutEffect(() => {
    const instance = editor.current;
    switching.current = true;
    const previous = instance.getModel();
    if (previous)
      views.current.set(previous.uri.path.slice(1), instance.saveViewState());
    const needed = files.filter((file) => (!readonly && !file.readonly) || file.path === activePath);
    for (const path of views.current.keys())
      if (!files.some((file) => file.path === path)) views.current.delete(path);
    for (const [path, model] of models.current)
      if (!needed.some((file) => file.path === path)) {
        if (instance.getModel() === model) instance.setModel(null);
        model.dispose();
        models.current.delete(path);
      }
    for (const file of needed) {
      let model = models.current.get(file.path);
      if (!model) {
        model = monaco.editor.createModel(
          file.source,
          /\.(?:go|gox)$/.test(file.path) ? "go" : "plaintext",
          monaco.Uri.from({
            scheme: "inmemory",
            authority: scope.replace(/[^a-z0-9]/gi, ""),
            path: "/" + file.path,
          }),
        );
        models.current.set(file.path, model);
      } else if (model.getValue() !== file.source) model.setValue(file.source);
      monaco.editor.setModelMarkers(
        model,
        "goxide",
        markers
          .filter((marker) => marker.path === file.path)
          .map(({ path, ...marker }) => ({
            ...marker,
            severity:
              marker.severity === "warning"
                ? monaco.MarkerSeverity.Warning
                : marker.severity === "note"
                  ? monaco.MarkerSeverity.Info
                  : monaco.MarkerSeverity.Error,
          })),
      );
    }
    const next = models.current.get(activePath) || null;
    if (next !== instance.getModel()) {
      instance.setModel(next);
      if (views.current.has(activePath))
        instance.restoreViewState(views.current.get(activePath));
    }
    instance.updateOptions({ readOnly: readonly || !!files.find((file) => file.path === activePath)?.readonly });
    switching.current = false;
  }, [files, activePath, readonly, markers]);
  useLayoutEffect(() => {
    if (!position || position.path !== activePath) return;
    const target = { lineNumber: position.startLineNumber, column: position.startColumn };
    editor.current.setPosition(target);
    editor.current.revealPositionInCenter(target);
    editor.current.focus();
  }, [position, activePath]);
  return <div className="monaco-host" ref={host} />;
}
