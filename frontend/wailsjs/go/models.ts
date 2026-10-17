export namespace app {
	
	export class RuntimeInfo {
	    goVersion: string;
	    os: string;
	    arch: string;
	    sqliteVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.goVersion = source["goVersion"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.sqliteVersion = source["sqliteVersion"];
	    }
	}

}

export namespace contracts {
	
	export class AppError {
	    code: string;
	    operationID?: string;
	    torrentID?: string;
	    params: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new AppError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.operationID = source["operationID"];
	        this.torrentID = source["torrentID"];
	        this.params = source["params"];
	    }
	}
	export class TorrentFile {
	    index: number;
	    path: string;
	    sizeBytes: string;
	    completedBytes: string;
	    wanted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TorrentFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.path = source["path"];
	        this.sizeBytes = source["sizeBytes"];
	        this.completedBytes = source["completedBytes"];
	        this.wanted = source["wanted"];
	    }
	}
	export class TorrentSnapshot {
	    id: string;
	    infohashV1?: string;
	    infohashV2?: string;
	    name: string;
	    status: string;
	    manualPaused: boolean;
	    uploadAllowed: boolean;
	    totalBytes?: string;
	    wantedBytes?: string;
	    completedBytes: string;
	    downloadBytesPerSecond: string;
	    uploadBytesPerSecond: string;
	    peerCount: number;
	    etaSeconds?: string;
	    savePath: string;
	    source: string;
	    addedAt: string;
	    fileCount?: number;
	    selectedFileCount?: number;
	    files: TorrentFile[];
	    errors: AppError[];
	
	    static createFrom(source: any = {}) {
	        return new TorrentSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.infohashV1 = source["infohashV1"];
	        this.infohashV2 = source["infohashV2"];
	        this.name = source["name"];
	        this.status = source["status"];
	        this.manualPaused = source["manualPaused"];
	        this.uploadAllowed = source["uploadAllowed"];
	        this.totalBytes = source["totalBytes"];
	        this.wantedBytes = source["wantedBytes"];
	        this.completedBytes = source["completedBytes"];
	        this.downloadBytesPerSecond = source["downloadBytesPerSecond"];
	        this.uploadBytesPerSecond = source["uploadBytesPerSecond"];
	        this.peerCount = source["peerCount"];
	        this.etaSeconds = source["etaSeconds"];
	        this.savePath = source["savePath"];
	        this.source = source["source"];
	        this.addedAt = source["addedAt"];
	        this.fileCount = source["fileCount"];
	        this.selectedFileCount = source["selectedFileCount"];
	        this.files = this.convertValues(source["files"], TorrentFile);
	        this.errors = this.convertValues(source["errors"], AppError);
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
	export class LibrarySnapshot {
	    runtimeID: string;
	    revision: string;
	    torrents: TorrentSnapshot[];
	
	    static createFrom(source: any = {}) {
	        return new LibrarySnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runtimeID = source["runtimeID"];
	        this.revision = source["revision"];
	        this.torrents = this.convertValues(source["torrents"], TorrentSnapshot);
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
	export class OperationStatus {
	    operationID: string;
	    runtimeID: string;
	    kind: string;
	    state: string;
	    error?: AppError;
	
	    static createFrom(source: any = {}) {
	        return new OperationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.operationID = source["operationID"];
	        this.runtimeID = source["runtimeID"];
	        this.kind = source["kind"];
	        this.state = source["state"];
	        this.error = this.convertValues(source["error"], AppError);
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

