import { API } from './bridge.js';

// The form windows are sized to their own content, so every time that content
// changes - a row hidden, an error line added - the page asks for a new window.
//
// The ask is a request, not an instruction: what a window's frame costs is a fact
// about the platform and the display, and the page cannot know it. Windows charges
// a frame that changes size with the display's scaling, and macOS sizes the frame
// when asked to resize but sized the content when asked to create, so the same
// number means two different windows. So the page asks, reads back what it got,
// and asks again for the difference - a few times at most.
//
// Both form windows are created with resizing off, so a height that does not match
// the form can only be the window's mistake, never the user's. That is what makes
// it safe to correct in either direction.
const maxTries = 4;

let asked = 0;
let tries = 0;
let timer = 0;

function wantedHeight() {
    const body = document.querySelector('.sheet-body');
    const actions = document.querySelector('.sheet-actions');
    // The two pixels are rounding, not slack. Every number on both sides of the
    // request is an integer - these two measurements, and the window height once
    // it is turned into physical pixels at this screen's scaling - so the sum can
    // land a fraction short of the content it was measured from. A box a fraction
    // too short is invisible except for what it triggers: the scroll area decides
    // it does overflow and takes a scrollbar, which narrows the fields beside it
    // and makes the shortfall real.
    return Math.max(body.scrollHeight, body.offsetHeight) + actions.offsetHeight + 2;
}

function settle() {
    clearTimeout(timer);
    // macOS animates a resize, so the height measured just after the ask is not
    // the height the window ends at. Judge it once it stops moving.
    timer = setTimeout(correct, 200);
}

function correct() {
    if (!tries) return;
    const wanted = wantedHeight();
    const got = window.innerHeight;
    // A window a few pixels taller than its form is the form sitting at the top
    // of it, which is what the spare room is for; one shorter is a scrollbar.
    if (got >= wanted && got <= wanted + 6) return;
    if (tries++ >= maxTries) return;
    // What the platform kept back on the last answer is the best estimate of what
    // it will keep back on this one. It is measured, not guessed, so the scaling
    // that makes Windows' frame three pixels wider cannot strand one.
    asked = wanted + Math.max(0, asked - got);
    API().FitFormWindow(asked);
    settle();
}

export function fitFormWindow() {
    asked = wantedHeight();
    tries = 1;
    API().FitFormWindow(asked);
    settle();
}

window.addEventListener('resize', settle);
