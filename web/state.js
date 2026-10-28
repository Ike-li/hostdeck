// The console's shared constants and its one mutable state object. Modules
// import state and write its fields; nothing else holds page state.
export const LANGS = ['en', 'zh-CN'];
export const LANG_KEY = 'hostdeck.lang';
export const STATUS_EVERY = 15_000;
export const IDLE_RELEASE = 10 * 60_000; // a page switched away from this long is unloaded, and loads again on its next visit
export const ARMED_FOR = 4000; // how long a button that needs a second click waits for it
export const PENDING_AGAIN = 2000; // how soon a card still being read is asked for again
export const LAYOUT_NOTE_FOR = 8000; // idle time before the saved notice and its one-step undo close
export const INTERNAL_DRAG = 'application/x-hostdeck-internal';
export const CATALOG = '.plugins'; // the plugins page's address, #/.plugins: no plugin ID starts with a dot
export const SEARCH_AFTER = 10; // at most this many plugins stay a plain list, without the search box

export const state = {
  lang: 'en',
  dict: {},
  plugins: [], // the sidebar entries
  status: {}, // plugin id -> up | down
  current: null, // the open sidebar entry, or null on the overview
  catalogOpen: false, // whether the plugins page is shown
  catalog: null, // the last answer of /api/catalog
  catalogNoteFor: null, // the plugin whose card the last result belongs on
  catalogAddRequested: false, // the overview add card should land at the add form
  pendingOverview: null, // a reading waiting until card controls lose focus
  overview: null, // the last answer of /api/cards
  failure: null, // why the backend could not be read
  detailFilters: new Map(), // plugin id -> session-only search and group choice
  frames: new Map(), // plugin id -> its iframe or detail view, kept so each page keeps its state
  clearing: false,
  clearFailure: null, // why the last clearing failed
  hiddenSince: new Map(), // plugin id -> when its page was switched away from
  notifyPush: new Map(), // plugin id -> what its port's notifications window last said the browser answered
  dragging: false, // whether a sidebar entry or a card is being dragged
  suppressClick: false, // a click that ends a drag is not an open
  layoutUndo: null, // the layout lists to post to undo the last move, widen, drag, hide or reset
  savingLayout: false,
  layoutEpoch: 0, // a save that answers late must not replace a newer one
  stored: { sidebar: [], cards: [], wide: [], hidden: [] }, // the layout file, hidden IDs included
  layoutKnown: false, // a hide before this is known would post only part of the hidden list
};
