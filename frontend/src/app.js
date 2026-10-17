// Wails runtime will be injected automatically
const API = () => window.go.main.App;

let state = null;
let platformInfo = null;
let nodeResults = {};   // idx -> last test text
let ctxIdx = -1;        // node under the context menu
let editIdx = -1;       // -1 = add, >=0 = edit

async function loadState() {
    state = await API().GetState();
    if (!platformInfo) {
        platformInfo = await API().GetPlatformInfo();
    }
    render();
}

function setStatus(text) {
    document.getElementById('statusText').textContent = text || '';
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

    // Ports with defaults
    document.getElementById('socksPort').value = state.socks_port || '10808';
    document.getElementById('httpPort').value = state.http_port || '10809';

    document.getElementById('modeSelect').value = state.proxy_mode || 'direct';
    document.getElementById('routingSelect').value = state.routing_mode || 'global';

    document.getElementById('allowLAN').checked = state.allow_lan;
    document.getElementById('startLogin').checked = state.start_login;
    document.getElementById('hideTray').checked = state.hide_tray;
    document.getElementById('tunSwitch').checked = !!state.tun_enabled;

    if (platformInfo && platformInfo.tunSupported) {
        document.getElementById('tunGroup').style.display = 'flex';
    }

    if (platformInfo && platformInfo.startLoginSupported) {
        document.getElementById('startLoginGroup').style.display = 'flex';
    }
    if (platformInfo && platformInfo.trayCanHide) {
        document.getElementById('hideTrayGroup').style.display = 'flex';
    }
    // System-proxy mode only exists where the OS proxy can actually be set.
    if (platformInfo && !platformInfo.systemProxySupported) {
        const opt = document.querySelector('#modeSelect option[value="system"]');
        if (opt) opt.remove();
    }
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
    document.getElementById('modalTitle').textContent = idx < 0 ? 'Add Node' : 'Edit Node';
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

// ---- node / mode / checkboxes ----

async function selectNode(idx) {
    try {
        await API().SelectNode(idx);
        await loadState();
    } catch (err) {
        setStatus('Switch failed: ' + err.message);
    }
}

document.getElementById('modeSelect').onchange = async (e) => {
    const mode = e.target.value;
    try {
        await API().SetProxyMode(mode);
    } catch (err) {
        setStatus('Switch failed: ' + err.message);
    }
    // Reload so the dropdown reflects the stored mode (a failed system-proxy
    // switch stays on the previous mode) and the engine reflects the change.
    await loadState();
};

document.getElementById('routingSelect').onchange = async (e) => {
    try {
        await API().SetRoutingMode(e.target.value);
    } catch (err) {
        setStatus('Routing switch failed: ' + err.message);
    }
    await loadState();
};

document.getElementById('allowLAN').onchange = async (e) => {
    await API().SetAllowLAN(e.target.checked);
    await loadState();
};

document.getElementById('startLogin').onchange = async (e) => {
    try {
        await API().SetStartAtLogin(e.target.checked);
    } catch (err) {
        setStatus('Start at login failed: ' + err.message);
    }
    await loadState();
};

document.getElementById('hideTray').onchange = async (e) => {
    await API().SetHideTray(e.target.checked);
    await loadState();
};

// Bringing the tunnel up or down touches the virtual adapter and the system
// resolver, and can wait on an elevation prompt - long enough that a second
// click during the first one would be a real hazard, not just a nuisance.
document.getElementById('tunSwitch').onchange = async (e) => {
    const sw = e.target;
    const on = sw.checked;
    sw.disabled = true;
    setStatus(on ? 'Enabling TUN...' : 'Disabling TUN...');
    try {
        await API().SetTun(on);
        setStatus('');
    } catch (err) {
        setStatus('TUN failed: ' + err.message);
    }
    sw.disabled = false;
    await loadState();
};

// ---- port inputs ----

function setupPortInput(inputId, setter) {
    const input = document.getElementById(inputId);
    input.onblur = async () => {
        await setter(input.value);
        await loadState();
    };
    input.onkeydown = (e) => {
        if (e.key === 'Enter') input.blur();
    };
}

setupPortInput('socksPort', (v) => API().SetSocksPort(v));
setupPortInput('httpPort', (v) => API().SetHttpPort(v));

// Initialize
loadState().catch((err) => setStatus('Load failed: ' + err.message));
