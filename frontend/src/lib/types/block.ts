// src/types/block.ts
import type { NodeProps, Node } from '@xyflow/svelte'; // Adjust based on svelte-flow's type exports
import type { Nullish } from '$lib/utils/utils';
import { z } from 'zod';
import type { BlockDefinition, BlockParameterValue } from '$lib/types/manifest';
import type { EdgeVariant } from '$lib/types/edge';

export type CtrlData = Record<
  string,
  BlockDefinition['parameters'] extends infer T
    ? T extends Record<string, infer U>
      ? U & {
          functionName: string;
          param: string;
          value: BlockParameterValue;
        }
      : never
    : never
>;

export type BlockData = {
  id: string;
  label: string;
  type: string;
  func: string;
  path: string;
  running?: boolean;
  failed?: boolean;
  ctrls: CtrlData;
  initCtrls?: CtrlData;
  inputs?: BlockDefinition['inputs'];
  outputs?: BlockDefinition['outputs'];
  pip_dependencies?: {
    name: string;
    v: Nullish<string>;
  }[];
  invalid?: boolean;
  colorStore?: string; // Add this if needed by ColorPickerNode
};

export type BlockNode = Node<BlockData>;
export type BlockProps = NodeProps<BlockNode>;

export const positionSchema = z.object({
  x: z.number(),
  y: z.number(),
});

export type EdgeData = {
  outputType: EdgeVariant;
};