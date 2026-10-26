import { $, el, feedback } from './dom.js';
import { t, pick } from './i18n.js';
import { closePanel } from './panel.js';
import { state } from './state.js';

// The proxy answers this address itself, on the service's own port.
const NOTIFICATIONS_PATH = '/.well-known/hostdeck/notifications';

// A browser keys a notification permission by the top-level page. A framed
// service is asked as the console, which no frame can prompt for, and its
// service worker, which runs without the console, as its own port. So the
// dialog has one step for each address, and each says that address's answer.
export function openNotifications(p) {
  closePanel();
  const dialog = $('page-editor');
  const head = el('header', 'editor-head');
  const title = el('h2', null, t('notify.title', { name: pick(p.name) }));
  title.id = 'editor-title';
  title.title = title.textContent;
  head.append(title);
  const description = el('p', null, t('notify.intro'));
  description.id = 'editor-description';
  const pageStep = step('page', t('notify.page.title'), t('notify.page.hint'), t('notify.allow'));
  const pushStep = step('push', t('notify.push.title'), t('notify.push.hint'), t('notify.allow'));
  const body = el('div', 'editor-body');
  body.append(description, pageStep, pushStep);
  const footer = el('footer', 'editor-actions');
  const done = el('button', 'btn', t('notify.close'));
  done.id = 'notify-close';
  done.type = 'button';
  done.addEventListener('click', () => dialog.close());
  footer.append(done);
  dialog.replaceChildren(head, body, footer);

  const served = new URL(p.url).origin;
  const showPage = (permission) => show('page', permission, location);
  const showPush = (permission) => show('push', permission, new URL(served));
  showPage('Notification' in window ? Notification.permission : 'unsupported');
  const showLastPush = () => showPush(state.notifyPush.get(p.id) ?? 'unchecked');
  showLastPush();
  // The console's own answer can change in the browser's site settings while
  // the dialog is open; the step follows it.
  let pageAnswer = null;
  navigator.permissions?.query({ name: 'notifications' }).then((status) => {
    if (!dialog.open) return;
    pageAnswer = status;
    status.onchange = () => showPage(Notification.permission);
  });

  $('notify-page-allow').addEventListener('click', async () => {
    feedback($('notify-page-status'), t('notify.asking'));
    showPage(await Notification.requestPermission());
  });
  let asking = null;
  let watching = null; // while the window is open: closed unanswered, the step stops waiting
  $('notify-push-allow').addEventListener('click', () => {
    const url = new URL(NOTIFICATIONS_PATH, served);
    url.searchParams.set('lang', state.lang);
    clearInterval(watching);
    // A blocked address is turned on from the address bar, which a full tab
    // is sure to show; asking needs no more than a small window.
    const blocked = state.notifyPush.get(p.id) === 'denied';
    asking = blocked ? open(url, 'hostdeck-notifications') : open(url, 'hostdeck-notifications', 'popup,width=440,height=320');
    if (!asking) return feedback($('notify-push-status'), t('notify.popupBlocked', { origin: location.origin }), true);
    feedback($('notify-push-status'), t('notify.asking'));
    watching = setInterval(() => {
      if (!asking.closed) return;
      clearInterval(watching);
      showLastPush();
    }, 500);
  });
  const onMessage = (e) => {
    if (e.origin !== served || e.source !== asking || e.data?.type !== 'hostdeck:notifications') return;
    clearInterval(watching);
    state.notifyPush.set(p.id, e.data.permission);
    showPush(e.data.permission);
  };
  addEventListener('message', onMessage);
  dialog.onclose = () => {
    removeEventListener('message', onMessage);
    clearInterval(watching);
    if (pageAnswer) pageAnswer.onchange = null;
    const trigger = $('service-more').querySelector('.more-trigger');
    if (state.current?.id === p.id && trigger.checkVisibility()) trigger.focus();
  };
  dialog.showModal();
  done.focus();
}

function step(kind, heading, hint, allow) {
  const section = el('section', 'notify-step');
  const button = el('button', 'btn', allow);
  button.id = `notify-${kind}-allow`;
  button.type = 'button';
  const status = el('p', 'notify-status');
  status.id = `notify-${kind}-status`;
  section.append(el('h3', null, heading), el('p', null, hint), status, button);
  return section;
}

// show words one step's answer. Only an undecided page step can still prompt.
// A blocked step says where its address is turned on: the page step in this
// page's address bar, the push step in its window, which waits there for it.
function show(kind, permission, address) {
  const status = $(`notify-${kind}-status`);
  const button = $(`notify-${kind}-allow`);
  const words = {
    granted: t('notify.on'),
    denied: t(`notify.${kind}.denied`, { origin: address.origin }),
    default: t('notify.notYet'),
    unchecked: t('notify.unchecked'),
    unsupported: t('notify.unsupported'),
  }[permission] ?? t('notify.notYet');
  feedback(status, words, permission === 'denied' || permission === 'unsupported');
  button.hidden = permission === 'granted' || permission === 'unsupported' || (kind === 'page' && permission === 'denied');
  button.textContent = kind === 'push' && permission === 'denied' ? t('notify.open', { host: address.host }) : t('notify.allow');
}
