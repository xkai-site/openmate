import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Edge, Node } from 'reactflow';
import { getFullTree } from '@/services/api/tree';
import type { TreeNodeResponse } from '@/types/models';
import dagre from 'dagre';

interface TreeNodeData {
  id: string;
  name?: string;
  status?: string;
  updated_at?: string;
}

function transformTreeToFlow(tree?: TreeNodeResponse | null): { nodes: Node<TreeNodeData>[]; edges: Edge[] } {
  if (!tree) return { nodes: [], edges: [] };

  const nodes: Node<TreeNodeData>[] = [];
  const edges: Edge[] = [];
  
  // First, extract all nodes and edges
  const walk = (node: TreeNodeResponse, parentId?: string): void => {
    nodes.push({
      id: node.id,
      type: 'treeNode',
      position: { x: 0, y: 0 }, // Initial position, will be updated by dagre
      data: {
        id: node.id,
        name: node.name,
        status: node.status,
        updated_at: node.updated_at,
      },
    });

    if (parentId) {
      edges.push({
        id: `${parentId}-${node.id}`,
        source: parentId,
        target: node.id,
        type: 'smoothstep',
        animated: node.status === 'running',
      });
    }

    if (node.children) {
      node.children.forEach((child) => walk(child, node.id));
    }
  };

  walk(tree);

  // Apply dagre layout
  const dagreGraph = new dagre.graphlib.Graph();
  dagreGraph.setDefaultEdgeLabel(() => ({}));
  
  const nodeWidth = 280;
  const nodeHeight = 80;
  
  // LR means Left-to-Right layout, suitable for trees
  dagreGraph.setGraph({ rankdir: 'LR', align: 'UL', nodesep: 50, ranksep: 100 });

  nodes.forEach((node) => {
    dagreGraph.setNode(node.id, { width: nodeWidth, height: nodeHeight });
  });

  edges.forEach((edge) => {
    dagreGraph.setEdge(edge.source, edge.target);
  });

  dagre.layout(dagreGraph);

  const layoutedNodes = nodes.map((node) => {
    const nodeWithPosition = dagreGraph.node(node.id);
    return {
      ...node,
      position: {
        x: nodeWithPosition.x - nodeWidth / 2,
        y: nodeWithPosition.y - nodeHeight / 2,
      },
    };
  });

  return { nodes: layoutedNodes, edges };
}

export function useAITree(rootId?: string) {
  const treeQuery = useQuery({
    queryKey: ['aitree', 'full-tree', rootId],
    queryFn: () => getFullTree(rootId),
    refetchInterval: 10000,
  });

  const flowData = useMemo(() => transformTreeToFlow(treeQuery.data), [treeQuery.data]);

  return {
    treeQuery,
    ...flowData,
  };
}