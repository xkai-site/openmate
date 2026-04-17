import { useMemo, useState } from 'react';

export function usePagination(initialPageSize = 10) {
  const [current, setCurrent] = useState(1);
  const [pageSize, setPageSize] = useState(initialPageSize);

  const offset = useMemo(() => (current - 1) * pageSize, [current, pageSize]);

  const onChange = (page: number, size: number) => {
    setCurrent(page);
    setPageSize(size);
  };

  const reset = () => {
    setCurrent(1);
  };

  return {
    current,
    pageSize,
    offset,
    onChange,
    reset,
  };
}