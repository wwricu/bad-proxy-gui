// Wails runtime will be injected automatically
const API = () => window.go.main.App;

let state = null;
let platformInfo = null;
let nodeResults = {};   // idx -> last test text
let ctxIdx = -1;        // node under the context menu
let editIdx = -1;       // -1 = add, >=0 = edit

// The status bar and the settings card name the same choices with the same
// words as the native settings window, so a mode is one thing on screen.
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
        const result = nodeResults[idx] ? ' · ' + nodeResults[idx] : '';
        item.innerHTML = `
            <div class="node-info">
                <div class="node-name">${escapeHtml(node.name)}</div>
                <div class="node-detail">${escapeHtml(node.protocol)}://${escapeHtml(node.server)}:${escapeHtml(node.port)}${escapeHtml(result)}</div>
            </div>
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

// ---- node form ----

// Mirrors the backend's visibleNodeFields: which optional fields apply.
function visibleFields(protocol, transport) {
    const f = { secret: protocol !== 'socks', authority: false, path: false, padding: false };
    if (transport === 'tls') f.authority = true;
    if (transport === 'xhttp') { f.authority = true; f.path = true; f.padding = true; }
    return f;
}

function applyFieldVisibility() {
    const f = visibleFields(document.getElementById('fProtocol').value, document.getElementById('fTransport').value);
    setRow('lSecret', 'fSecret', f.secret);
    setRow('lAuthority', 'fAuthority', f.authority);
    setRow('lPath', 'fPath', f.path);
    setRow('lPadding', 'fPadding', f.padding);
}

function setRow(labelId, inputId, visible) {
    document.getElementById(labelId).style.display = visible ? 'block' : 'none';
    document.getElementById(inputId).style.display = visible ? 'block' : 'none';
}

function openForm(idx) {
    editIdx = idx;
    const n = idx < 0 ? { name: '', protocol: 'vless', server: '', port: '', secret: '', transport: 'xhttp', path: '', authority: '', padding: '' } : state.nodes[idx];
    document.getElementById('fName').value = n.name || '';
    document.getElementById('fProtocol').value = n.protocol || 'vless';
    document.getElementById('fServer').value = n.server || '';
    document.getElementById('fPort').value = n.port || '';
    document.getElementById('fSecret').value = n.secret || '';
    document.getElementById('fTransport').value = n.transport || 'xhttp';
    document.getElementById('fAuthority').value = n.authority || '';
    document.getElementById('fPath').value = n.path || '';
    document.getElementById('fPadding').value = n.padding || '';
    applyFieldVisibility();
    document.getElementById('modalOverlay').style.display = 'flex';
    document.getElementById('fName').focus();
}

document.getElementById('modalCancel').onclick = () => {
    document.getElementById('modalOverlay').style.display = 'none';
};

document.getElementById('modalSave').onclick = async () => {
    const node = {
        name: document.getElementById('fName').value.trim(),
        protocol: document.getElementById('fProtocol').value,
        server: document.getElementById('fServer').value.trim(),
        port: document.getElementById('fPort').value.trim(),
        secret: document.getElementById('fSecret').value.trim(),
        transport: document.getElementById('fTransport').value,
        path: document.getElementById('fPath').value.trim(),
        authority: document.getElementById('fAuthority').value.trim(),
        padding: document.getElementById('fPadding').value.trim(),
    };
    if (!node.name || !node.server || !node.port) {
        const shown = await API().InfoDialog('Bad Proxy', 'Name, address and port are required');
        if (!shown) setStatus('Name, address and port are required');
        return;
    }
    let saved = true;
    try {
        if (editIdx < 0) {
            await API().AddNode(node);
        } else {
            await API().UpdateNode(editIdx, node);
        }
    } catch (err) {
        // A restart error still means the node reached the state file.
        setStatus(err.message);
        saved = false;
    }
    if (saved) document.getElementById('modalOverlay').style.display = 'none';
    nodeResults = {};
    await loadState();
};

document.getElementById('fProtocol').onchange = applyFieldVisibility;
document.getElementById('fTransport').onchange = applyFieldVisibility;
document.getElementById('addNodeRow').onclick = () => editNode(-1);

// editNode picks the editor backend: a real OS dialog where supported
// (Windows), otherwise the in-page HTML modal.
async function editNode(idx) {
    if (platformInfo && platformInfo.nodeDialog) {
        try {
            if (idx < 0) await API().AddNodeUI();
            else await API().EditNodeUI(idx);
        } catch (err) {
            // The node is already on disk by the time a restart fails, so the
            // list must still refresh; only the engine status is in doubt.
            setStatus(err.message);
        }
        nodeResults = {};
        await loadState();
        return;
    }
    openForm(idx);
}

// ---- settings window ----

// openSettings picks the backend the same way the node editor does: a real OS
// window where Wails can hand one over (Windows), the in-page card elsewhere.
async function openSettings() {
    if (platformInfo && platformInfo.settingsDialog) {
        try {
            await API().SettingsUI();
        } catch (err) {
            // Everything up to the failing step did apply, and the state file
            // already holds the draft, so the bar has to be re-read either way.
            setStatus(err.message);
        }
        await loadState();
        return;
    }
    document.getElementById('sHttpPort').value = state.http_port || '10809';
    document.getElementById('sSocksPort').value = state.socks_port || '10808';
    document.getElementById('sMode').value = state.proxy_mode || 'direct';
    document.getElementById('sRouting').value = state.routing_mode || 'global';
    document.getElementById('sTun').checked = !!state.tun_enabled;
    document.getElementById('sLAN').checked = !!state.allow_lan;
    document.getElementById('sLogin').checked = !!state.start_login;
    document.getElementById('sTray').checked = !!state.hide_tray;

    document.getElementById('sTunRow').style.display =
        platformInfo && platformInfo.tunSupported ? 'flex' : 'none';
    document.getElementById('sLoginRow').style.display =
        platformInfo && platformInfo.startLoginSupported ? 'flex' : 'none';
    document.getElementById('sTrayRow').style.display =
        platformInfo && platformInfo.trayCanHide ? 'flex' : 'none';
    // System-proxy mode only exists where the OS proxy can actually be set.
    const sysOpt = document.querySelector('#sMode option[value="system"]');
    if (sysOpt && platformInfo && !platformInfo.systemProxySupported) sysOpt.remove();

    syncTunRow();
    document.getElementById('settingsOverlay').style.display = 'flex';
    document.getElementById('sHttpPort').focus();
}

// syncTunRow keeps the tunnel honest about the mode: direct has no node for it
// to carry traffic through, so the box comes unticked and unusable until a
// proxy mode is picked. The native window does exactly the same.
function syncTunRow() {
    const row = document.getElementById('sTunRow');
    const tun = document.getElementById('sTun');
    const off = document.getElementById('sMode').value === 'direct';
    tun.disabled = off;
    if (off) tun.checked = false;
    row.classList.toggle('muted', off);
}

document.getElementById('sMode').onchange = syncTunRow;

document.getElementById('gearBtn').onclick = openSettings;

document.getElementById('settingsCancel').onclick = () => {
    document.getElementById('settingsOverlay').style.display = 'none';
    setStatus('');
};

document.getElementById('settingsOK').onclick = async () => {
    const draft = {
        http_port: document.getElementById('sHttpPort').value.trim(),
        socks_port: document.getElementById('sSocksPort').value.trim(),
        proxy_mode: document.getElementById('sMode').value,
        routing_mode: document.getElementById('sRouting').value,
        tun_enabled: document.getElementById('sTun').checked,
        allow_lan: document.getElementById('sLAN').checked,
        start_login: document.getElementById('sLogin').checked,
        hide_tray: document.getElementById('sTray').checked,
    };
    // Bringing the tunnel up or down touches the virtual adapter and the system
    // resolver, and can wait on an elevation prompt, so the button is locked
    // while OK is in flight.
    const btn = document.getElementById('settingsOK');
    btn.disabled = true;
    setStatus('Applying...');
    try {
        await API().ApplySettings(draft);
        document.getElementById('settingsOverlay').style.display = 'none';
        setStatus('');
    } catch (err) {
        // The card stays open with what the user typed: an error here means
        // nothing of it took effect except what ApplySettings got to.
        setStatus(err.message);
    }
    btn.disabled = false;
    await loadState();
};

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
