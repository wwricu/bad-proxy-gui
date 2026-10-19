import { API } from './bridge.js';
import { Events } from "/wails/runtime.js";
import * as sheet from './sheet.js';

let state = null;
let platformInfo = null;
let nodeResults = {};   // idx -> last test text
let ctxIdx = -1;        // node under the context menu

// The status bar and the settings window name the same choices with the same
// words, so a mode is one thing on screen.
const MODE_LABELS = { direct: 'Direct', proxy: 'Proxy', system: 'System proxy' };
const ROUTING_LABELS = { global: 'Global', 'cn-whitelist': 'Bypass CN', 'gfw-blacklist': 'Bypass GFW' };

async function loadState() {
    state = await API().GetState();
    if (!platformInfo) {
        platformInfo = await API().GetPlatformInfo();
    }
    render();
}

let statusTimer = 0;

// ttlMs is optional: a status line given one clears itself, so a transient
// failure does not stay on the bar looking like a current condition.
function setStatus(text, ttlMs) {
    clearTimeout(statusTimer);
    statusTimer = 0;
    document.getElementById('statusText').textContent = text || '';
    if (text && ttlMs) {
        statusTimer = setTimeout(() => setStatus(''), ttlMs);
    }
}

function render() {
    // Node list (Go bindings serialize fields in snake_case)
    const nodeList = document.getElementById('nodeList');
    nodeList.innerHTML = '';
    (state.nodes || []).forEach((node, idx) => {
        const item = document.createElement('div');
        item.className = 'node-item' + (idx === state.active ? ' active' : '');
        item.innerHTML = `
            <div class="node-info">
                <div class="node-name">${escapeHtml(node.name)}</div>
                <div class="node-detail">${escapeHtml(node.protocol)}://${escapeHtml(node.server)}:${escapeHtml(node.port)} · ${escapeHtml(node.transport || 'tls')}</div>
            </div>
            ${resultChip(nodeResults[idx] || '')}
        `;
        item.onclick = () => selectNode(idx);
        item.ondblclick = () => editNode(idx);
        item.oncontextmenu = (e) => { e.preventDefault(); nodeContextMenu(idx, e.clientX, e.clientY); };
        nodeList.appendChild(item);
    });

    // Read-only: what the engine is actually listening on and running.
    document.getElementById('roHttp').textContent = state.http_port || '10809';
    document.getElementById('roSocks').textContent = state.socks_port || '10808';
    const mode = state.proxy_mode || 'direct';
    const routing = state.routing_mode || 'global';
    document.getElementById('roMode').textContent = MODE_LABELS[mode] || mode;
    document.getElementById('roRouting').textContent = ROUTING_LABELS[routing] || routing;
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text == null ? '' : String(text);
    return div.innerHTML;
}

function escapeAttr(text) {
    return escapeHtml(text).replace(/"/g, '&quot;');
}

// A test failure reads like "Failed: dial tcp: lookup x: no such host" and the
// row has a couple of centimetres for it, so half a sentence on the corner is
// worse than one word. The corner says which of the three states the node is
// in; the reason waits on the chip for whoever asked for it.
function resultChip(result) {
    if (!result) return '';
    if (result === 'Testing…') {
        return '<div class="node-result pending">Testing…</div>';
    }
    if (result.startsWith('Failed')) {
        return `<div class="node-result failed" title="${escapeAttr(result)}">Failed</div>`;
    }
    return `<div class="node-result">${escapeHtml(result)}</div>`;
}

// ---- context menu ----

// The row menu's items, in the one order both of its renderings draw them in:
// the native Win32 menu is built from this list joined by newlines, the DOM
// fallback in index.html lists the same three.
const ctxMenuLabels = ['Edit', 'Test', 'Delete'];

// On Windows the backend pops a real Win32 shortcut menu at the cursor; the
// DOM menu below stays as the fallback for macOS/Linux.
async function nodeContextMenu(idx, x, y) {
    if (platformInfo && platformInfo.nativeContextMenu) {
        let sel = -1;
        try {
            sel = await API().ShowContextMenu(ctxMenuLabels.join('\n'));
        } catch (err) {
            setStatus('Menu failed: ' + err.message);
            return;
        }
        if (sel === 0) editNode(idx);
        else if (sel === 1) ctxActionTest(idx);
        else if (sel === 2) ctxActionDelete(idx);
        return;
    }
    openCtxMenu(idx, x, y);
}

function openCtxMenu(idx, x, y) {
    ctxIdx = idx;
    const menu = document.getElementById('ctxMenu');
    menu.style.display = 'block';
    menu.style.left = Math.min(x, window.innerWidth - menu.offsetWidth - 4) + 'px';
    menu.style.top = Math.min(y, window.innerHeight - menu.offsetHeight - 4) + 'px';
}

function closeCtxMenu() {
    document.getElementById('ctxMenu').style.display = 'none';
}

document.addEventListener('click', closeCtxMenu);
document.addEventListener('contextmenu', (e) => {
    if (!e.target.closest('.node-item')) closeCtxMenu();
});
window.addEventListener('blur', closeCtxMenu);

async function ctxActionTest(idx) {
    nodeResults[idx] = 'Testing…';
    render();
    try {
        const latency = await API().TestNode(idx);
        nodeResults[idx] = latency;
        setStatus('');
    } catch (err) {
        nodeResults[idx] = 'Failed: ' + err.message;
    }
    render();
}

// The node waiting to be deleted, or -1. The sheet that asks is the only thing
// that sets it, and it clears it whether the answer was yes or no.
let pendingDelete = -1;

function ctxActionDelete(idx) {
    // One sentence, one line where it fits: the point of the card is that the
    // name is short enough to read whole before answering.
    document.getElementById('deleteQuestion').textContent =
        'Delete node "' + state.nodes[idx].name + '"?';
    pendingDelete = idx;
    sheet.show('deleteSheet');
}

document.getElementById('deleteCancel').onclick = () => sheet.close();

document.getElementById('deleteOK').onclick = async () => {
    const idx = pendingDelete;
    sheet.close();
    try {
        await API().DeleteNode(idx);
        nodeResults = {};
        await loadState();
    } catch (err) {
        setStatus('Delete failed: ' + err.message);
    }
};

document.getElementById('ctxTest').onclick = () => {
    closeCtxMenu();
    if (ctxIdx >= 0) ctxActionTest(ctxIdx);
};

document.getElementById('ctxEdit').onclick = () => {
    closeCtxMenu();
    if (ctxIdx >= 0) editNode(ctxIdx);
};

document.getElementById('ctxDelete').onclick = () => {
    closeCtxMenu();
    if (ctxIdx >= 0) ctxActionDelete(ctxIdx);
};

// ---- forms ----

// The editor and the settings form are windows of their own, so opening one is
// a call into the backend, which is also what keeps a second copy from
// stacking up. Their writes go through the same bound methods this page uses.
async function openForm(fn, ...args) {
    try {
        await API()[fn](...args);
    } catch (err) {
        setStatus('Window failed: ' + err.message);
    }
}

document.getElementById('addNodeBtn').onclick = () => openForm('AddNodeUI');
document.getElementById('gearBtn').onclick = () => openForm('SettingsUI');

function editNode(idx) {
    openForm('EditNodeUI', idx);
}

// A form window writes the state this page displays, and the backend signals
// the window closing, so that is the signal to reread. Test results are dropped
// with it: applying settings restarts the engine, so the last latency is no
// longer a measurement of what is running.
Events.On('state-changed', () => {
    nodeResults = {};
    loadState().catch((err) => setStatus('Load failed: ' + err.message));
});

// The delete card is the one form that stays inside this window.
sheet.onSheetClosed(() => { pendingDelete = -1; });

// ---- nodes ----

async function selectNode(idx) {
    try {
        await API().SelectNode(idx);
        await loadState();
    } catch (err) {
        setStatus('Switch failed: ' + err.message);
    }
}

// ---- initialize ----

// The Windows self-test opens this page with ?selftest=1 and drives it with
// script. Testing and deleting a node both start in a native OS menu, which a
// script cannot reach, so the two actions behind those items are handed over.
if (new URLSearchParams(window.location.search).has('selftest')) {
    window.selftest = {
        testNode: ctxActionTest,
        askDelete: ctxActionDelete,
        // The list the native menu is built from. Its order is a decision, so
        // the self-test reads it back rather than trust the call site.
        menuLabels: ctxMenuLabels,
    };
}

loadState().catch((err) => setStatus('Load failed: ' + err.message));
