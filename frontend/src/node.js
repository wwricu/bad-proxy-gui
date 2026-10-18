import { API } from './bridge.js';
import * as sheet from './sheet.js';

// The editor for one node, as a sheet over the list. The draft is this sheet's
// own; Save is the only write.

let editIdx = -1;

function setFormMsg(text) {
    document.getElementById('fMsg').textContent = text || '';
}

// Mirrors the backend's visibleNodeFields: which optional fields apply.
function visibleFields(protocol, transport) {
    const f = { secret: protocol !== 'socks', authority: false, path: false, padding: false };
    if (transport === 'tls') f.authority = true;
    if (transport === 'xhttp') { f.authority = true; f.path = true; f.padding = true; }
    return f;
}

function setRow(labelId, inputId, visible) {
    document.getElementById(labelId).style.display = visible ? 'block' : 'none';
    document.getElementById(inputId).style.display = visible ? 'block' : 'none';
}

function applyFieldVisibility() {
    const f = visibleFields(document.getElementById('fProtocol').value, document.getElementById('fTransport').value);
    setRow('lSecret', 'fSecret', f.secret);
    setRow('lAuthority', 'fAuthority', f.authority);
    setRow('lPath', 'fPath', f.path);
    setRow('lPadding', 'fPadding', f.padding);
}

async function fill(index) {
    const state = await API().GetState();
    const n = index < 0 ? {} : (state.nodes || [])[index] || {};
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
    document.getElementById('fName').focus();
}

export function openNodeSheet(index) {
    editIdx = index;
    document.getElementById('nodeSheetTitle').textContent = index < 0 ? 'Add node' : 'Edit node';
    document.getElementById('modalSave').disabled = false;
    setFormMsg('');
    sheet.show('nodeSheet');
    fill(index).catch((err) => setFormMsg('Load failed: ' + err.message));
}

document.getElementById('fProtocol').onchange = applyFieldVisibility;
document.getElementById('fTransport').onchange = applyFieldVisibility;
document.getElementById('modalCancel').onclick = sheet.close;
document.getElementById('nodeCloseTop').onclick = sheet.close;

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
        // sheet stays open only to say that.
        setFormMsg(err.message);
        btn.disabled = false;
        return;
    }
    sheet.close();
};
