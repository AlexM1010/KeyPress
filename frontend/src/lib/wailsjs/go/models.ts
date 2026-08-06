export namespace backend {
	
	export class Edge {
	    id: string;
	    source: string;
	    target: string;
	    sourceHandle?: string;
	    targetHandle?: string;
	    type?: string;
	
	    static createFrom(source: any = {}) {
	        return new Edge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source = source["source"];
	        this.target = source["target"];
	        this.sourceHandle = source["sourceHandle"];
	        this.targetHandle = source["targetHandle"];
	        this.type = source["type"];
	    }
	}
	export class Node {
	    id: string;
	    type: string;
	    data: {[key: string]: any};
	    position: {[key: string]: number};
	
	    static createFrom(source: any = {}) {
	        return new Node(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.data = source["data"];
	        this.position = source["position"];
	    }
	}
	export class FlowData {
	    id?: string;
	    name?: string;
	    nodes: Node[];
	    edges: Edge[];
	
	    static createFrom(source: any = {}) {
	        return new FlowData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.nodes = this.convertValues(source["nodes"], Node);
	        this.edges = this.convertValues(source["edges"], Edge);
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
	
	export class ProjectSummary {
	    id: string;
	    name: string;
	    nodeCount: number;
	    edgeCount: number;
	    modifiedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.nodeCount = source["nodeCount"];
	        this.edgeCount = source["edgeCount"];
	        this.modifiedAt = source["modifiedAt"];
	    }
	}

}

