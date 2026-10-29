import { API } from './bridge.js';
import { fitFormWindow } from './formfit.js';
import { Window } from "/wails/runtime.js";

// The settings window is the only settings UI on every platform. Nothing here
// applies until OK.

const MODE_LABELS = { direct: 'Direct', proxy: 'Proxy', system: 'System' };
const ROUTING_LABELS = { global: 'Global', 'cn-whitelist': 'Bypass CN', 'gfw-blacklist': 'Bypass GFW' };

// A setting that failed to take effect is read beside the box that caused it,
// which is the part of the window the user is looking at when it fails.
function setFormMsg(text, busy) {
    const el = document.getElementById('sMsg');
    el.textContent = text || '';
    el.classList.toggle('busy', !!busy);
    fitFormWindow();
}

async function init() {
    const [draft, platform, rulesVersion] = await Promise.all([
        API().SettingsDraft(), API().GetPlatformInfo(), API().RulesVersion(),
    ]);

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
    // The one item Windows calls the tray is what macOS calls the menu bar, and
    // the row should name it the way the person reading it does.
    document.querySelector('#sTrayRow label').textContent =
        platform.trayIsMenuBar ? 'Hide menu-bar icon' : 'Hide tray icon';
    // System-proxy mode only exists where the OS proxy can actually be set.
    if (!platform.systemProxySupported) {
        document.querySelector('#sMode option[value="system"]').remove();
    }

    // Which build is already installed, so a click that would only fetch the
    // same file again is not a blind one. An app that has never replaced its
    // bundled copy has no recorded date and says nothing.
    if (rulesVersion) {
        const m = document.getElementById('sRulesMsg');
        m.classList.add('busy');
        m.textContent = 'Current: ' + rulesVersion;
    }

    document.getElementById('sHttpPort').focus();
    // Which rows the OS offers is only known after the platform read, so this is
    // the first moment the page knows its own height.
    fitFormWindow();
}

document.getElementById('settingsCancel').onclick = () => Window.Close();

// Updating rules first asks the server whether there is anything new, and only
// downloads and restarts when there is. Neither step is instant, so the button
// is disabled for the duration and the outcome is read inline instead of
// closing the window.
//
// A dead network used to look like a frozen app for the backend's full 8s+30s
// budget, so after 12s this says once - re-saying it would re-fit the window -
// that the network may be down.
document.getElementById('sUpdateRules').onclick = async () => {
    const btn = document.getElementById('sUpdateRules');
    const msg = document.getElementById('sRulesMsg');
    btn.disabled = true;
    msg.classList.add('busy');
    msg.textContent = 'Updating...';
    let nagged = false;
    const started = Date.now();
    const tick = setInterval(() => {
        if (nagged || Date.now() - started < 12000) return;
        nagged = true;
        msg.textContent = 'Still waiting - the network may be down.';
        fitFormWindow();
    }, 1000);
    try {
        msg.classList.remove('busy');
        msg.textContent = await API().UpdateRules();
    } catch (err) {
        msg.textContent = err.message;
    } finally {
        clearInterval(tick);
        btn.disabled = false;
        fitFormWindow();
    }
};

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
