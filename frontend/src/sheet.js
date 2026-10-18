// The delete confirmation is the one form that stays inside the main window: it
// is a question about a row drawn right there, and a window of its own would be
// all title bar for one sentence.
//
// Nothing dismisses the card by clicking the scrim. The exits are Cancel, the
// cross and Esc.

let current = null;
let onClosed = null;

function hide() {
    if (!current) return;
    const cur = current;
    current = null;
    cur.el.hidden = true;
    if (onClosed) onClosed();
}

export function show(id) {
    hide();
    const el = document.getElementById(id);
    el.hidden = false;
    current = { el };
}

export function close() {
    hide();
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
