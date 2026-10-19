import { API } from './bridge.js';

// Both form windows are sized to their own content, so every time that content
// changes - a row hidden, an error line added - the page has to ask for a new
// window. What it sends is a client height: what the frame costs on top of that
// is a per-platform fact only the backend has.
//
// The two extra pixels are not slack for its own sake. Every number here is an
// integer - scrollHeight and offsetHeight are the rounded box, and the window
// height is rounded again when it is turned into physical pixels at this
// screen's scaling - so the sum can land a fraction short of the content it was
// measured from. A box a fraction too short is invisible except for what it
// triggers: the scroll area decides it does overflow and takes a scrollbar,
// which narrows the fields beside it and makes the shortfall real.
export function fitFormWindow() {
    const body = document.querySelector('.sheet-body');
    const actions = document.querySelector('.sheet-actions');
    const height = Math.max(body.scrollHeight, body.offsetHeight) + actions.offsetHeight + 2;
    if (height > 0) API().FitFormWindow(height);
}
