import { API } from './bridge.js';
import { Window } from "/wails/runtime.js";

// The settings window is the only settings UI on every platform, so this is the
// code that used to have an HTML-card twin and a native Win32 twin. Nothing
// here applies until OK.

const MODE_LABELS = { direct: 'Direct', proxy: 'Proxy', system: 'System proxy' };
const ROUTING_LABELS = { global: 'Global', 'cn-whitelist': 'Bypass CN', 'gfw-blacklist': 'Bypass GFW' };

// setFormMsg writes this window's own message line. A setting that failed to
// take effect belongs beside the box that caused it, and this window is what
// the user is looking at when it fails.
function setFormMsg(text, busy) {
    const el = document.getElementById('sMsg');
    el.textContent = text || '';
    el.classList.toggle('busy', !!busy);
}

// syncTunRow keeps the tunnel honest about the mode: direct has no node for it
// to carry traffic through, so the box comes unticked and unusable until a
// proxy mode is picked.
function syncTunRow() {
    const row = document.getElementById('sTunRow');
    const tun = document.getElementById('sTun');
    const off = document.getElementById('sMode').value === 'direct';
    tun.disabled = off;
    if (off) tun.checked = false;
    row.classList.toggle('muted', off);
}

async function init() {
    // The draft, not GetState: the tunnel is whatever is actually running, so a
    // run where up() failed must not open with the box ticked.
    const [draft, platform] = await Promise.all([API().SettingsDraft(), API().GetPlatformInfo()]);

    document.getElementById('sHttpPort').value = draft.http_port || '10809';
    document.getElementById('sSocksPort').value = draft.socks_port || '10808';
    document.getElementById('sMode').value = MODE_LABELS[draft.proxy_mode] ? draft.proxy_mode : 'direct';
    document.getElementById('sRouting').value = ROUTING_LABELS[draft.routing_mode] ? draft.routing_mode : 'global';
    document.getElementById('sTun').checked = !!draft.tun_enabled;
    document.getElementById('sLAN').checked = !!draft.allow_lan;
    document.getElementById('sLogin').checked = !!draft.start_login;

    // Rows that only make sense where the OS offers the thing behind them.
    document.getElementById('sTunRow').style.display = platform.tunSupported ? 'flex' : 'none';
    document.getElementById('sLoginRow').style.display = platform.startLoginSupported ? 'flex' : 'none';
    document.getElementById('sTrayRow').style.display = platform.trayCanHide ? 'flex' : 'none';
    document.getElementById('sTray').checked = !!draft.hide_tray;
    // System-proxy mode only exists where the OS proxy can actually be set.
    if (!platform.systemProxySupported) {
        document.querySelector('#sMode option[value="system"]').remove();
    }

    syncTunRow();
    document.getElementById('sHttpPort').focus();
}

document.getElementById('sMode').onchange = syncTunRow;

document.getElementById('settingsCancel').onclick = () => Window.Close();

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
    setFormMsg('Applying...', true);
    try {
        await API().ApplySettings(draft);
    } catch (err) {
        // The window stays open with what the user typed: the tunnel is the
        // setting that fails after the engine is already moving, and its reason
        // is worthless once the window is gone.
        setFormMsg(err.message);
        btn.disabled = false;
        // Some of the draft may have reached the disk before the refusal, so
        // reread rather than keep claiming the old values.
        const draft = await API().SettingsDraft();
        document.getElementById('sHttpPort').value = draft.http_port;
        document.getElementById('sSocksPort').value = draft.socks_port;
        document.getElementById('sTun').checked = !!draft.tun_enabled;
        return;
    }
    setFormMsg('');
    // The main page watches for this window closing and rereads the state.
    await Window.Close();
};

init().catch((err) => setFormMsg('Load failed: ' + err.message));
