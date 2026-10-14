// deno-lint-ignore-file prefer-const
export function persistedState<T>(key: string, defaults: T) {
  let value = { value: defaults };
  try {
    const stored = localStorage.getItem(key);
    const parsed = stored ? JSON.parse(stored) : null;
    if (
      parsed &&
      Object.hasOwn(parsed, "value") &&
      (defaults === undefined || typeof parsed.value === typeof defaults) &&
      (parsed.value !== null || defaults === null)
    ) {
      value = { value: parsed.value };
    }
  } catch {}

  let state = $state(value);
  $effect.root(() => {
    $effect(() => {
      const serialized = JSON.stringify(state);
      try {
        localStorage.setItem(key, serialized);
      } catch {}
    });
  });

  return {
    get current() {
      return state.value;
    },
    set current(value: T) {
      state.value = value;
    },
    reset() {
      state.value = defaults;
    },
    state,
  };
}
