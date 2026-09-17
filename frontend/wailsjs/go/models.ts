export namespace collector {
	
	export class Item {
	    scanID: string;
	    nodeID: number;
	    path: string;
	    name: string;
	    size: number;
	    kind: number;
	    scanRoot: string;
	    asAdmin: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scanID = source["scanID"];
	        this.nodeID = source["nodeID"];
	        this.path = source["path"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.kind = source["kind"];
	        this.scanRoot = source["scanRoot"];
	        this.asAdmin = source["asAdmin"];
	    }
	}
	export class State {
	    items: Item[];
	    total: number;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], Item);
	        this.total = source["total"];
	        this.count = source["count"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AppInfo {
	    version: string;
	    build: string;
	    bundled: boolean;
	    bundleID: string;
	    teamID: string;
	    caps: platform.Caps;
	
	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.build = source["build"];
	        this.bundled = source["bundled"];
	        this.bundleID = source["bundleID"];
	        this.teamID = source["teamID"];
	        this.caps = this.convertValues(source["caps"], platform.Caps);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CacheEntry {
	    key: string;
	    root: string;
	    savedAt: number;
	    nodes: number;
	    bytes: number;
	
	    static createFrom(source: any = {}) {
	        return new CacheEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.root = source["root"];
	        this.savedAt = source["savedAt"];
	        this.nodes = source["nodes"];
	        this.bytes = source["bytes"];
	    }
	}
	export class CachedScan {
	    found: boolean;
	    key: string;
	    root: string;
	    savedAt: number;
	    nodes: number;
	    bytes: number;
	    ageMillis: number;
	
	    static createFrom(source: any = {}) {
	        return new CachedScan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.found = source["found"];
	        this.key = source["key"];
	        this.root = source["root"];
	        this.savedAt = source["savedAt"];
	        this.nodes = source["nodes"];
	        this.bytes = source["bytes"];
	        this.ageMillis = source["ageMillis"];
	    }
	}
	export class HelperStatus {
	    installed: boolean;
	    running: boolean;
	    version: number;
	    build: string;
	    compatible: boolean;
	    registered: string;
	    err?: string;
	    registration: string;
	    needsApproval: boolean;
	    bundled: boolean;
	    advice?: string;
	
	    static createFrom(source: any = {}) {
	        return new HelperStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.running = source["running"];
	        this.version = source["version"];
	        this.build = source["build"];
	        this.compatible = source["compatible"];
	        this.registered = source["registered"];
	        this.err = source["err"];
	        this.registration = source["registration"];
	        this.needsApproval = source["needsApproval"];
	        this.bundled = source["bundled"];
	        this.advice = source["advice"];
	    }
	}
	export class SnapshotInfo {
	    name: string;
	    date: string;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.date = source["date"];
	    }
	}
	export class HiddenSpaceReport {
	    volume: string;
	    mount: string;
	    total: number;
	    used: number;
	    free: number;
	    visible: number;
	    hasScan: boolean;
	    scanning: boolean;
	    purgeable: number;
	    snapshots: SnapshotInfo[];
	    snapshotBytes: number;
	    protected: number;
	    inaccessible: number;
	    unaccounted: number;
	    notes: string[];
	
	    static createFrom(source: any = {}) {
	        return new HiddenSpaceReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.volume = source["volume"];
	        this.mount = source["mount"];
	        this.total = source["total"];
	        this.used = source["used"];
	        this.free = source["free"];
	        this.visible = source["visible"];
	        this.hasScan = source["hasScan"];
	        this.scanning = source["scanning"];
	        this.purgeable = source["purgeable"];
	        this.snapshots = this.convertValues(source["snapshots"], SnapshotInfo);
	        this.snapshotBytes = source["snapshotBytes"];
	        this.protected = source["protected"];
	        this.inaccessible = source["inaccessible"];
	        this.unaccounted = source["unaccounted"];
	        this.notes = source["notes"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ItemRef {
	    scanID: string;
	    nodeID: number;
	
	    static createFrom(source: any = {}) {
	        return new ItemRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scanID = source["scanID"];
	        this.nodeID = source["nodeID"];
	    }
	}
	export class Preview {
	    nodeID: number;
	    name: string;
	    path: string;
	    kind: number;
	    size: number;
	    mtime: number;
	    image: string;
	    utType: string;
	    ext: string;
	
	    static createFrom(source: any = {}) {
	        return new Preview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeID = source["nodeID"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.kind = source["kind"];
	        this.size = source["size"];
	        this.mtime = source["mtime"];
	        this.image = source["image"];
	        this.utType = source["utType"];
	        this.ext = source["ext"];
	    }
	}
	export class Result {
	    succeeded: string[];
	    failed: Record<string, string>;
	    bytes: number;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.succeeded = source["succeeded"];
	        this.failed = source["failed"];
	        this.bytes = source["bytes"];
	    }
	}

}

export namespace platform {
	
	export class AccessStatus {
	    granted: boolean;
	    probe: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new AccessStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.granted = source["granted"];
	        this.probe = source["probe"];
	        this.detail = source["detail"];
	    }
	}
	export class Caps {
	    os: string;
	    preview: boolean;
	    properties: boolean;
	    snapshots: boolean;
	    purgeable: boolean;
	    helper: boolean;
	    helperInstall: boolean;
	    fullDiskAccess: boolean;
	    revealLabel: string;
	    trashLabel: string;
	    trashVerb: string;
	    propertiesLabel: string;
	    modKey: string;
	
	    static createFrom(source: any = {}) {
	        return new Caps(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.os = source["os"];
	        this.preview = source["preview"];
	        this.properties = source["properties"];
	        this.snapshots = source["snapshots"];
	        this.purgeable = source["purgeable"];
	        this.helper = source["helper"];
	        this.helperInstall = source["helperInstall"];
	        this.fullDiskAccess = source["fullDiskAccess"];
	        this.revealLabel = source["revealLabel"];
	        this.trashLabel = source["trashLabel"];
	        this.trashVerb = source["trashVerb"];
	        this.propertiesLabel = source["propertiesLabel"];
	        this.modKey = source["modKey"];
	    }
	}
	export class Volume {
	    name: string;
	    path: string;
	    uuid: string;
	    total: number;
	    free: number;
	    used: number;
	    availRaw: number;
	    purgeable: number;
	    removable: boolean;
	    internal: boolean;
	    network: boolean;
	    readOnly: boolean;
	    isRoot: boolean;
	    fs: string;
	    icon?: string;
	
	    static createFrom(source: any = {}) {
	        return new Volume(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.uuid = source["uuid"];
	        this.total = source["total"];
	        this.free = source["free"];
	        this.used = source["used"];
	        this.availRaw = source["availRaw"];
	        this.purgeable = source["purgeable"];
	        this.removable = source["removable"];
	        this.internal = source["internal"];
	        this.network = source["network"];
	        this.readOnly = source["readOnly"];
	        this.isRoot = source["isRoot"];
	        this.fs = source["fs"];
	        this.icon = source["icon"];
	    }
	}

}

export namespace prefs {
	
	export class Prefs {
	    version: number;
	    base2: boolean;
	    theme: string;
	    colorScheme: string;
	    showHidden: boolean;
	    exclusions: string[];
	    updateChannel: string;
	    autoCheckUpdates: boolean;
	    sendStats: boolean;
	    installID: string;
	    allowPermanentDelete: boolean;
	    confirmTrash: boolean;
	    useCache: boolean;
	    useBulkScan: boolean;
	    minArcFraction: number;
	    rings: number;
	    uiScale: number;
	    minItemSize: number;
	    sidebarWidth: number;
	
	    static createFrom(source: any = {}) {
	        return new Prefs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.base2 = source["base2"];
	        this.theme = source["theme"];
	        this.colorScheme = source["colorScheme"];
	        this.showHidden = source["showHidden"];
	        this.exclusions = source["exclusions"];
	        this.updateChannel = source["updateChannel"];
	        this.autoCheckUpdates = source["autoCheckUpdates"];
	        this.sendStats = source["sendStats"];
	        this.installID = source["installID"];
	        this.allowPermanentDelete = source["allowPermanentDelete"];
	        this.confirmTrash = source["confirmTrash"];
	        this.useCache = source["useCache"];
	        this.useBulkScan = source["useBulkScan"];
	        this.minArcFraction = source["minArcFraction"];
	        this.rings = source["rings"];
	        this.uiScale = source["uiScale"];
	        this.minItemSize = source["minItemSize"];
	        this.sidebarWidth = source["sidebarWidth"];
	    }
	}

}

export namespace scanstore {
	
	export class Summary {
	    id: string;
	    root: string;
	    label: string;
	    volumeUUID?: string;
	    asAdmin: boolean;
	    status: string;
	    files: number;
	    dirs: number;
	    bytes: number;
	    inaccessible: number;
	    currentPath?: string;
	    startedAt: number;
	    finishedAt?: number;
	    stalled: boolean;
	    stalledMs?: number;
	    fromCache: boolean;
	    cachedAt?: number;
	    err?: string;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.root = source["root"];
	        this.label = source["label"];
	        this.volumeUUID = source["volumeUUID"];
	        this.asAdmin = source["asAdmin"];
	        this.status = source["status"];
	        this.files = source["files"];
	        this.dirs = source["dirs"];
	        this.bytes = source["bytes"];
	        this.inaccessible = source["inaccessible"];
	        this.currentPath = source["currentPath"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.stalled = source["stalled"];
	        this.stalledMs = source["stalledMs"];
	        this.fromCache = source["fromCache"];
	        this.cachedAt = source["cachedAt"];
	        this.err = source["err"];
	    }
	}

}

export namespace tree {
	
	export class NodeSummary {
	    id: number;
	    name: string;
	    size: number;
	    logical: number;
	    kind: number;
	    flags: number;
	    files: number;
	    dirs: number;
	    mtime: number;
	    hasKids: boolean;
	    fraction: number;
	    err?: string;
	
	    static createFrom(source: any = {}) {
	        return new NodeSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.logical = source["logical"];
	        this.kind = source["kind"];
	        this.flags = source["flags"];
	        this.files = source["files"];
	        this.dirs = source["dirs"];
	        this.mtime = source["mtime"];
	        this.hasKids = source["hasKids"];
	        this.fraction = source["fraction"];
	        this.err = source["err"];
	    }
	}
	export class ChildPage {
	    rows: NodeSummary[];
	    total: number;
	    hidden: number;
	    hiddenBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new ChildPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows = this.convertValues(source["rows"], NodeSummary);
	        this.total = source["total"];
	        this.hidden = source["hidden"];
	        this.hiddenBytes = source["hiddenBytes"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class VNode {
	    id: number;
	    name: string;
	    size: number;
	    logical: number;
	    kind: number;
	    flags: number;
	    files: number;
	    dirs: number;
	    mtime: number;
	    hasKids: boolean;
	    other?: number;
	    err?: string;
	    children?: VNode[];
	
	    static createFrom(source: any = {}) {
	        return new VNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.logical = source["logical"];
	        this.kind = source["kind"];
	        this.flags = source["flags"];
	        this.files = source["files"];
	        this.dirs = source["dirs"];
	        this.mtime = source["mtime"];
	        this.hasKids = source["hasKids"];
	        this.other = source["other"];
	        this.err = source["err"];
	        this.children = this.convertValues(source["children"], VNode);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewportSlice {
	    focus: VNode;
	    ancestors: VNode[];
	    rootTotal: number;
	    scanning: boolean;
	    sorted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ViewportSlice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.focus = this.convertValues(source["focus"], VNode);
	        this.ancestors = this.convertValues(source["ancestors"], VNode);
	        this.rootTotal = source["rootTotal"];
	        this.scanning = source["scanning"];
	        this.sorted = source["sorted"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace updater {
	
	export class State {
	    supported: boolean;
	    checking: boolean;
	    updateAvailable: boolean;
	    currentVersion: string;
	    latestVersion?: string;
	    releaseNotes?: string;
	    downloadURL?: string;
	    lastChecked?: number;
	    err?: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported = source["supported"];
	        this.checking = source["checking"];
	        this.updateAvailable = source["updateAvailable"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.releaseNotes = source["releaseNotes"];
	        this.downloadURL = source["downloadURL"];
	        this.lastChecked = source["lastChecked"];
	        this.err = source["err"];
	        this.reason = source["reason"];
	    }
	}

}

