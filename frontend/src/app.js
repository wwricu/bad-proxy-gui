import { API } from './bridge.js';
import * as sheet from './sheet.js';
import { openNodeSheet } from './node.js';
import { openSettingsSheet } from './settings.js';

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
        const result = nodeResults[idx] || '';
        const resultClass = result === 'Testing…' ? ' pending'
            : result.startsWith('Failed') ? ' failed' : '';
        item.innerHTML = `
            <div class="node-info">
                <div class="node-name">${escapeHtml(node.name)}</div>
                <div class="node-detail">${escapeHtml(node.protocol)}://${escapeHtml(node.server)}:${escapeHtml(node.port)} · ${escapeHtml(node.transport || 'tls')}</div>
            </div>
            ${result ? `<div class="node-result${resultClass}">${escapeHtml(result)}</div>` : ''}
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

// ---- context menu ----

// On Windows the backend pops a real Win32 shortcut menu at the cursor; the
// DOM menu below stays as the fallback for macOS/Linux.
async function nodeContextMenu(idx, x, y) {
    if (platformInfo && platformInfo.nativeContextMenu) {
        let sel = -1;
        try {
            sel = await API().ShowContextMenu('Test\nEdit\nDelete');
        } catch (err) {
            setStatus('Menu failed: ' + err.message);
            return;
        }
        if (sel === 0) ctxActionTest(idx);
        else if (sel === 1) editNode(idx);
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

async function ctxActionDelete(idx) {
    let ok = false;
    try {
        ok = await API().ConfirmDialog('Bad Proxy', 'Delete node "' + state.nodes[idx].name + '"?');
    } catch (err) {
        setStatus('Delete failed: ' + err.message);
        return;
    }
    if (!ok) return;
    try {
        await API().DeleteNode(idx);
        nodeResults = {};
        await loadState();
    } catch (err) {
        setStatus('Delete failed: ' + err.message);
    }
}

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

// Both forms are sheets over this window, opened here and written through the
// same bound methods this page would have used.
document.getElementById('addNodeBtn').onclick = () => openNodeSheet(-1);
document.getElementById('gearBtn').onclick = openSettingsSheet;

function editNode(idx) {
    openNodeSheet(idx);
}

// A sheet writes the state this page displays, so closing one is the signal to
// reread. Test results are dropped with it: applying settings restarts the
// engine, so the last latency is no longer a measurement of what is running.
sheet.onSheetClosed(() => {
    nodeResults = {};
    loadState().catch((err) => setStatus('Load failed: ' + err.message));
});

// ---- nodes ----

async function selectNode(idx) {
    try {
        await API().SelectNode(idx);
        await loadState();
    } catch (err) {
        setStatus('Switch failed: ' + err.message);
    }
}

// Initialize
loadState().catch((err) => setStatus('Load failed: ' + err.message));
