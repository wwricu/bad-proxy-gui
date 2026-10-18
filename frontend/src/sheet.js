// The node editor and the settings form are sheets over the main window, and
// only one of them can be up at a time: both write the same state.
//
// Nothing dismisses a sheet by clicking the scrim. A mis-click that threw away
// a half-typed node is a worse accident than one more click to close it, so the
// exits are Cancel, the cross and Esc.

let current = null;
let locked = false;
let onClosed = null;

function hide() {
    if (!current) return;
    const cur = current;
    current = null;
    cur.el.hidden = true;
    if (onClosed) onClosed();
}

export function show(id) {
    if (locked) return;
    hide();
    const el = document.getElementById(id);
    el.hidden = false;
    current = { el };
}

export function close() {
    if (locked) return;
    hide();
}

// A sheet whose OK is still in flight must not be dismissed: applying settings
// can be waiting on an elevation prompt, and the answer still arrives.
export function lock(busy) {
    locked = !!busy;
}

export function onSheetClosed(fn) {
    onClosed = fn;
}

document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && current) {
        e.preventDefault();
        close();
    }
});
