import { API } from './bridge.js';
import { fitFormWindow } from './formfit.js';
import { Window } from "/wails/runtime.js";

// The editor for one node, in a window of its own. Which node is in the URL:
// ?index=N edits that entry, no index means a new one. The draft is this
// window's own; Save is the only write.

const editIdx = (() => {
    const raw = new URL(location.href).searchParams.get('index');
    const n = raw === null ? -1 : parseInt(raw, 10);
    return Number.isInteger(n) ? n : -1;
})();

function setFormMsg(text) {
    document.getElementById('fMsg').textContent = text || '';
    // The line is part of the form: a window sized for the fields alone would
    // show the refusal by squeezing them.
    fitFormWindow();
}

// Which optional fields apply to a protocol+transport, and why:
//   - secret   used by btp/vless/trojan; the socks handshake ignores it.
//   - tls      SNI (authority) only. TLS is implied by the transport itself.
//   - xhttp    path, authority (Host), request shape (mode).
// This is the only copy of the rule: the engine reads whatever fields the form
// sends, and a second copy in Go would be a second place to be wrong.
function visibleFields(protocol, transport) {
    const f = { secret: protocol !== 'socks', authority: false, path: false, mode: false };
    if (transport === 'tls') f.authority = true;
    if (transport === 'xhttp') { f.authority = true; f.path = true; f.mode = true; }
    return f;
}

function setRow(labelId, inputId, visible) {
    document.getElementById(labelId).style.display = visible ? 'block' : 'none';
    document.getElementById(inputId).style.display = visible ? 'block' : 'none';
}

// The window is sized to the form, so it has to be asked again whenever the
// form changes size. A hidden row left the window as tall as the tallest form:
// the fields flow from the top, so the buttons slid up and a band of bare window
// opened under them.
function applyFieldVisibility() {
    const f = visibleFields(document.getElementById('fProtocol').value, document.getElementById('fTransport').value);
    setRow('lSecret', 'fSecret', f.secret);
    setRow('lAuthority', 'fAuthority', f.authority);
    setRow('lPath', 'fPath', f.path);
    setRow('lMode', 'fMode', f.mode);
    fitFormWindow();
}

async function init() {
    const state = await API().GetState();
    const n = editIdx < 0 ? {} : (state.nodes || [])[editIdx] || {};
    document.getElementById('fName').value = n.name || '';
    document.getElementById('fProtocol').value = n.protocol || 'vless';
    document.getElementById('fServer').value = n.server || '';
    document.getElementById('fPort').value = n.port || '';
    document.getElementById('fSecret').value = n.secret || '';
    document.getElementById('fTransport').value = n.transport || 'xhttp';
    document.getElementById('fAuthority').value = n.authority || '';
    document.getElementById('fPath').value = n.path || '';
    document.getElementById('fMode').value = n.mode || 'stream-one';
    applyFieldVisibility();
    document.getElementById('fName').focus();
}

document.getElementById('fProtocol').onchange = applyFieldVisibility;
document.getElementById('fTransport').onchange = applyFieldVisibility;
document.getElementById('modalCancel').onclick = () => Window.Close();

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
        mode: document.getElementById('fMode').value,
    };
    if (!node.name || !node.server || !node.port) {
        setFormMsg('Name, address and port are required');
        return;
    }
    const btn = document.getElementById('modalSave');
    btn.disabled = true;
    setFormMsg('');
    try {
        if (editIdx < 0) {
            await API().AddNode(node);
        } else {
            await API().UpdateNode(editIdx, node);
        }
    } catch (err) {
        // A restart error still means the node reached the state file, so the
        // window stays open only to say that.
        setFormMsg(err.message);
        btn.disabled = false;
        return;
    }
    // Closing is what tells the list to reread; the hook on this window fires
    // for the Cancel button and the native cross the same way.
    await Window.Close();
};

init().catch((err) => setFormMsg('Load failed: ' + err.message));
