import { API } from './bridge.js';
import { Window } from "/wails/runtime.js";

// The settings window is the only settings UI on every platform. Nothing here
// applies until OK.

const MODE_LABELS = { direct: 'Direct', proxy: 'Proxy', system: 'System proxy' };
const ROUTING_LABELS = { global: 'Global', 'cn-whitelist': 'Bypass CN', 'gfw-blacklist': 'Bypass GFW' };

// A setting that failed to take effect is read beside the box that caused it,
// which is the part of the window the user is looking at when it fails.
function setFormMsg(text, busy) {
    const el = document.getElementById('sMsg');
    el.textContent = text || '';
    el.classList.toggle('busy', !!busy);
}

async function init() {
    const [draft, platform] = await Promise.all([API().SettingsDraft(), API().GetPlatformInfo()]);

    document.getElementById('sHttpPort').value = draft.http_port || '10809';
    document.getElementById('sSocksPort').value = draft.socks_port || '10808';
    document.getElementById('sMode').value = MODE_LABELS[draft.proxy_mode] ? draft.proxy_mode : 'direct';
    document.getElementById('sRouting').value = ROUTING_LABELS[draft.routing_mode] ? draft.routing_mode : 'global';
    document.getElementById('sLAN').checked = !!draft.allow_lan;
    document.getElementById('sLogin').checked = !!draft.start_login;

    // Rows that only make sense where the OS offers the thing behind them.
    document.getElementById('sLoginRow').style.display = platform.startLoginSupported ? 'flex' : 'none';
    document.getElementById('sTrayRow').style.display = platform.trayCanHide ? 'flex' : 'none';
    document.getElementById('sTray').checked = !!draft.hide_tray;
    // System-proxy mode only exists where the OS proxy can actually be set.
    if (!platform.systemProxySupported) {
        document.querySelector('#sMode option[value="system"]').remove();
    }

    document.getElementById('sHttpPort').focus();
}

document.getElementById('settingsCancel').onclick = () => Window.Close();

document.getElementById('settingsOK').onclick = async () => {
    const draft = {
        http_port: document.getElementById('sHttpPort').value.trim(),
        socks_port: document.getElementById('sSocksPort').value.trim(),
        proxy_mode: document.getElementById('sMode').value,
        routing_mode: document.getElementById('sRouting').value,
        allow_lan: document.getElementById('sLAN').checked,
        start_login: document.getElementById('sLogin').checked,
        hide_tray: document.getElementById('sTray').checked,
    };
    // Applying restarts the engine, which is not instant, so OK cannot be
    // pressed twice at the same draft.
    const btn = document.getElementById('settingsOK');
    btn.disabled = true;
    setFormMsg('Applying...', true);
    try {
        await API().ApplySettings(draft);
    } catch (err) {
        // The window stays open with what the user typed: the reason is worth
        // nothing once the window is gone.
        setFormMsg(err.message);
        btn.disabled = false;
        // Some of the draft may have reached the disk before the refusal, so
        // reread rather than keep claiming the old values.
        const applied = await API().SettingsDraft();
        document.getElementById('sHttpPort').value = applied.http_port;
        document.getElementById('sSocksPort').value = applied.socks_port;
        return;
    }
    // Closing is what tells the list to reread.
    await Window.Close();
};

init().catch((err) => setFormMsg('Load failed: ' + err.message));
