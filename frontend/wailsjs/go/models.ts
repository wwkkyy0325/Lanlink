export namespace main {
	
	export class AppSettings {
	    downloadDir: string;
	    askSaveLocation: boolean;
	    customRelays: string[];
	    useDoH: boolean;
	    transportMode: string;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.downloadDir = source["downloadDir"];
	        this.askSaveLocation = source["askSaveLocation"];
	        this.customRelays = source["customRelays"];
	        this.useDoH = source["useDoH"];
	        this.transportMode = source["transportMode"];
	    }
	}
	export class PairedPeer {
	    peerId: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new PairedPeer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.peerId = source["peerId"];
	        this.name = source["name"];
	    }
	}

}

export namespace models {
	
	export class Device {
	    id: string;
	    name: string;
	    ip: string;
	    port: number;
	    // Go type: time
	    lastSeen: any;
	    online: boolean;
	    source: string;
	    groups: string[];
	    p2pId?: string;
	
	    static createFrom(source: any = {}) {
	        return new Device(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.ip = source["ip"];
	        this.port = source["port"];
	        this.lastSeen = this.convertValues(source["lastSeen"], null);
	        this.online = source["online"];
	        this.source = source["source"];
	        this.groups = source["groups"];
	        this.p2pId = source["p2pId"];
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
	export class Group {
	    id: string;
	    code: string;
	    name: string;
	    members: string[];
	    encrypted: boolean;
	    key: string;
	    // Go type: time
	    created: any;
	
	    static createFrom(source: any = {}) {
	        return new Group(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.code = source["code"];
	        this.name = source["name"];
	        this.members = source["members"];
	        this.encrypted = source["encrypted"];
	        this.key = source["key"];
	        this.created = this.convertValues(source["created"], null);
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
	export class Message {
	    id: string;
	    deviceId: string;
	    deviceName: string;
	    content: string;
	    // Go type: time
	    time: any;
	    direction: string;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.deviceId = source["deviceId"];
	        this.deviceName = source["deviceName"];
	        this.content = source["content"];
	        this.time = this.convertValues(source["time"], null);
	        this.direction = source["direction"];
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
	export class SharedFile {
	    id: string;
	    shareId: string;
	    fileName: string;
	    fileSize: number;
	    senderId: string;
	    senderIP: string;
	    senderName: string;
	
	    static createFrom(source: any = {}) {
	        return new SharedFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.shareId = source["shareId"];
	        this.fileName = source["fileName"];
	        this.fileSize = source["fileSize"];
	        this.senderId = source["senderId"];
	        this.senderIP = source["senderIP"];
	        this.senderName = source["senderName"];
	    }
	}
	export class TransferRecord {
	    id: string;
	    deviceId: string;
	    deviceName: string;
	    fileName: string;
	    fileSize: number;
	    direction: string;
	    status: string;
	    // Go type: time
	    time: any;
	
	    static createFrom(source: any = {}) {
	        return new TransferRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.deviceId = source["deviceId"];
	        this.deviceName = source["deviceName"];
	        this.fileName = source["fileName"];
	        this.fileSize = source["fileSize"];
	        this.direction = source["direction"];
	        this.status = source["status"];
	        this.time = this.convertValues(source["time"], null);
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

export namespace p2p {
	
	export class UPnPResult {
	    enabled: boolean;
	    protocol: string;
	    internalPort: number;
	    externalPort: number;
	    externalIP: string;
	    localIP: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new UPnPResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.protocol = source["protocol"];
	        this.internalPort = source["internalPort"];
	        this.externalPort = source["externalPort"];
	        this.externalIP = source["externalIP"];
	        this.localIP = source["localIP"];
	        this.error = source["error"];
	    }
	}

}

