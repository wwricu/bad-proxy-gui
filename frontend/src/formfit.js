import { API } from './bridge.js';

// The form windows are sized to their own content, so every time that content
// changes - a row hidden, an error line added - the page asks for a new window.
//
// The ask is a request, not an instruction: what a window's frame costs is a fact
// about the platform and the display, and the page cannot know it. Windows charges
// a frame that changes size with the display's scaling, and macOS sizes the frame
// when asked to resize but sized the content when asked to create, so the same
// number means two different windows. So the page asks, reads back what it got,
// and asks again for the difference.
//
// The difference is signed, because the guess can be wrong in either direction and
// both are visible: a window that comes out shorter than the form gets a
// scrollbar, and one that comes out taller parks its buttons above a band of bare
// window. What the platform kept back is measured from the last answer, so the
// second ask is right whether the first guess was too small or too large.
//
// macOS animates a resize, so an answer read while the window is moving is not
// the answer - and correcting against it restarts the animation from wherever it
// had got to. So the page waits for the height to stop changing before it judges
// it.
//
// The budget for correcting is a number of rounds, not a length of time. Each
// round costs an animated resize plus the wait for it to settle, and how long
// that takes is AppKit's business on a machine that may be a virtual one; a
// deadline measured in milliseconds can run out with the window still short,
// which is the visible failure, while a count of rounds is the same work on
// every machine.
//
// Both form windows are created with resizing off, so a height that does not match
// the form can only be the window's mistake, never the user's. That is what makes
// it safe to keep correcting on every resize.
const settleTime = 150;
const maxRounds = 8;

let asked = 0;
let rounds = 0;
let timer = 0;

// What the last judgement saw, kept for the self-test to print. The loop is a
// control system whose only outward sign is a window height, so a wrong height
// on its own says nothing about which of its readings was wrong.
//
// "done" is the part that cannot be inferred from the height either: a window
// that has just reached the right size is a different fact from a loop that gave
// up short of it, and the self-test reads the page before the loop's next
// judgement is due, so it needs the loop to say which of the two it saw.
window.selffit = { asked: 0, wanted: 0, got: 0, kept: 0, rounds: 0, done: true };

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

function ask(height) {
    asked = height;
    window.selffit.done = false;
    API().FitFormWindow(height);
    watch();
}

function judge() {
    if (!asked) return;
    const wanted = wantedHeight();
    const got = window.innerHeight;
    // A window up to six pixels taller than its form is the form sitting at the
    // top of it, which is what the spare room is for. Anything else is a frame
    // that was not what the page guessed, and the gap between what was asked for
    // and what arrived is that frame, measured rather than assumed.
    //
    // The three numbers the self-test reads apart from the height: what was asked,
    // what arrived, and how many rounds it has taken. They separate a fit that
    // converged from one that ran out of rounds, which the height alone cannot.
    window.selffit.asked = asked;
    window.selffit.wanted = wanted;
    window.selffit.got = got;
    window.selffit.kept = asked - got;
    window.selffit.rounds = rounds;
    if ((got >= wanted && got <= wanted + 6) || rounds >= maxRounds) {
        asked = 0;
        window.selffit.done = true;
        return;
    }
    rounds++;
    ask(wanted + (asked - got));
}

function watch() {
    clearTimeout(timer);
    const height = window.innerHeight;
    // Two reads the same distance apart, and the window has stopped moving.
    timer = setTimeout(() => {
        if (window.innerHeight !== height) { watch(); return; }
        judge();
    }, settleTime);
}

export function fitFormWindow() {
    rounds = 0;
    ask(wantedHeight());
}

window.addEventListener('resize', watch);
