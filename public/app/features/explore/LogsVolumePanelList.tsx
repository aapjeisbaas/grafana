import { css } from '@emotion/css';
import { groupBy } from 'lodash';
import React, { useMemo } from 'react';

import {
  AbsoluteTimeRange,
  DataFrame,
  DataQueryResponse,
  EventBus,
  GrafanaTheme2,
  LoadingState,
  SplitOpen,
  TimeZone,
} from '@grafana/data';
import { Button, InlineField, useStyles2 } from '@grafana/ui';

import { getLogsVolumeDimensions, isLogsVolumeLimited } from '../logs/utils';

import { LogsVolumePanel } from './LogsVolumePanel';
import { SupplementaryResultError } from './SupplementaryResultError';

type Props = {
  logsVolumeData: DataQueryResponse | undefined;
  absoluteRange: AbsoluteTimeRange;
  timeZone: TimeZone;
  splitOpen: SplitOpen;
  width: number;
  onUpdateTimeRange: (timeRange: AbsoluteTimeRange) => void;
  onLoadLogsVolume: () => void;
  onHiddenSeriesChanged: (hiddenSeries: string[]) => void;
  eventBus: EventBus;
};

export const LogsVolumePanelList = ({
  logsVolumeData,
  absoluteRange,
  onUpdateTimeRange,
  width,
  onLoadLogsVolume,
  onHiddenSeriesChanged,
  eventBus,
  splitOpen,
  timeZone,
}: Props) => {
  const {
    logVolumes,
    maximum: allLogsVolumeMaximum,
    range: alignedAbsoluteRange,
  } = useMemo(() => {
    const data = logsVolumeData?.data || [];
    const logVolumes = groupBy(data, 'meta.custom.sourceQuery.refId');
    return {
      ...getLogsVolumeDimensions(data, absoluteRange),
      logVolumes,
    };
  }, [logsVolumeData, absoluteRange]);

  const styles = useStyles2(getStyles);

  const numberOfLogVolumes = Object.keys(logVolumes).length;

  const containsZoomed = Object.values(logVolumes).some((data: DataFrame[]) => {
    const zoomRatio = logsLevelZoomRatio(data, absoluteRange);
    return !isLogsVolumeLimited(data) && zoomRatio && zoomRatio < 1;
  });

  if (logsVolumeData?.state === LoadingState.Loading) {
    return <span>Loading...</span>;
  }
  if (logsVolumeData?.error !== undefined) {
    return <SupplementaryResultError error={logsVolumeData.error} title="Failed to load log volume for this query" />;
  }
  return (
    <div className={styles.listContainer}>
      {Object.keys(logVolumes).map((name, index) => {
        const logsVolumeData = { data: logVolumes[name] };
        return (
          <LogsVolumePanel
            key={index}
            absoluteRange={alignedAbsoluteRange}
            allLogsVolumeMaximum={allLogsVolumeMaximum}
            width={width}
            logsVolumeData={logsVolumeData}
            onUpdateTimeRange={onUpdateTimeRange}
            timeZone={timeZone}
            splitOpen={splitOpen}
            onLoadLogsVolume={onLoadLogsVolume}
            // TODO: Support filtering level from multiple log levels
            onHiddenSeriesChanged={numberOfLogVolumes > 1 ? () => {} : onHiddenSeriesChanged}
            eventBus={eventBus}
          />
        );
      })}
      {containsZoomed && (
        <div className={styles.extraInfoContainer}>
          <InlineField label="Reload log volume" transparent>
            <Button size="xs" icon="sync" variant="secondary" onClick={onLoadLogsVolume} id="reload-volume" />
          </InlineField>
        </div>
      )}
    </div>
  );
};

const getStyles = (theme: GrafanaTheme2) => {
  return {
    listContainer: css`
      padding-top: 10px;
    `,
    extraInfoContainer: css`
      display: flex;
      justify-content: end;
      position: absolute;
      right: 5px;
      top: 5px;
    `,
    oldInfoText: css`
      font-size: ${theme.typography.bodySmall.fontSize};
      color: ${theme.colors.text.secondary};
    `,
  };
};

function logsLevelZoomRatio(
  logsVolumeData: DataFrame[] | undefined,
  selectedTimeRange: AbsoluteTimeRange
): number | undefined {
  const dataRange = logsVolumeData && logsVolumeData[0] && logsVolumeData[0].meta?.custom?.absoluteRange;
  return dataRange ? (selectedTimeRange.from - selectedTimeRange.to) / (dataRange.from - dataRange.to) : undefined;
}
