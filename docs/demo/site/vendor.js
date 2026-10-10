// vendor.js: a date utility bundle. The shop calls one function (formatDate);
// the coverage demo reports the rest as unused bytes.
(function (global) {
  "use strict";
  const MS = { second: 1e3, minute: 6e4, hour: 36e5, day: 864e5, week: 6048e5, month: 2629746e3, quarter: 7889238e3, year: 31556952e3 };
  const lib = {};
  lib.formatDate = function formatDate(d) {
    return d.getFullYear() + "-" + String(d.getMonth() + 1).padStart(2, "0") + "-" + String(d.getDate()).padStart(2, "0");
  };
  lib.addSecond = function addSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return new Date(t + step);
  };
  lib.addMinute = function addMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return new Date(t + step);
  };
  lib.addHour = function addHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return new Date(t + step);
  };
  lib.addDay = function addDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return new Date(t + step);
  };
  lib.addWeek = function addWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return new Date(t + step);
  };
  lib.addMonth = function addMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return new Date(t + step);
  };
  lib.addQuarter = function addQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return new Date(t + step);
  };
  lib.addYear = function addYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return new Date(t + step);
  };
  lib.subtractSecond = function subtractSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return new Date(t - step);
  };
  lib.subtractMinute = function subtractMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return new Date(t - step);
  };
  lib.subtractHour = function subtractHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return new Date(t - step);
  };
  lib.subtractDay = function subtractDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return new Date(t - step);
  };
  lib.subtractWeek = function subtractWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return new Date(t - step);
  };
  lib.subtractMonth = function subtractMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return new Date(t - step);
  };
  lib.subtractQuarter = function subtractQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return new Date(t - step);
  };
  lib.subtractYear = function subtractYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return new Date(t - step);
  };
  lib.startOfSecond = function startOfSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.startOfMinute = function startOfMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.startOfHour = function startOfHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.startOfDay = function startOfDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.startOfWeek = function startOfWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.startOfMonth = function startOfMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.startOfQuarter = function startOfQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.startOfYear = function startOfYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.endOfSecond = function endOfSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.endOfMinute = function endOfMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.endOfHour = function endOfHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.endOfDay = function endOfDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.endOfWeek = function endOfWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.endOfMonth = function endOfMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.endOfQuarter = function endOfQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.endOfYear = function endOfYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.isSameSecond = function isSameSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) === Math.floor(Date.now() / step);
  };
  lib.isSameMinute = function isSameMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) === Math.floor(Date.now() / step);
  };
  lib.isSameHour = function isSameHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) === Math.floor(Date.now() / step);
  };
  lib.isSameDay = function isSameDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) === Math.floor(Date.now() / step);
  };
  lib.isSameWeek = function isSameWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) === Math.floor(Date.now() / step);
  };
  lib.isSameMonth = function isSameMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) === Math.floor(Date.now() / step);
  };
  lib.isSameQuarter = function isSameQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) === Math.floor(Date.now() / step);
  };
  lib.isSameYear = function isSameYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) === Math.floor(Date.now() / step);
  };
  lib.isBeforeSecond = function isBeforeSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) < Math.floor(Date.now() / step);
  };
  lib.isBeforeMinute = function isBeforeMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) < Math.floor(Date.now() / step);
  };
  lib.isBeforeHour = function isBeforeHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) < Math.floor(Date.now() / step);
  };
  lib.isBeforeDay = function isBeforeDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) < Math.floor(Date.now() / step);
  };
  lib.isBeforeWeek = function isBeforeWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) < Math.floor(Date.now() / step);
  };
  lib.isBeforeMonth = function isBeforeMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) < Math.floor(Date.now() / step);
  };
  lib.isBeforeQuarter = function isBeforeQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) < Math.floor(Date.now() / step);
  };
  lib.isBeforeYear = function isBeforeYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) < Math.floor(Date.now() / step);
  };
  lib.isAfterSecond = function isAfterSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) > Math.floor(Date.now() / step);
  };
  lib.isAfterMinute = function isAfterMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) > Math.floor(Date.now() / step);
  };
  lib.isAfterHour = function isAfterHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) > Math.floor(Date.now() / step);
  };
  lib.isAfterDay = function isAfterDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) > Math.floor(Date.now() / step);
  };
  lib.isAfterWeek = function isAfterWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) > Math.floor(Date.now() / step);
  };
  lib.isAfterMonth = function isAfterMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) > Math.floor(Date.now() / step);
  };
  lib.isAfterQuarter = function isAfterQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) > Math.floor(Date.now() / step);
  };
  lib.isAfterYear = function isAfterYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return Math.floor(t / step) > Math.floor(Date.now() / step);
  };
  lib.diffInSecond = function diffInSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.diffInMinute = function diffInMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.diffInHour = function diffInHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.diffInDay = function diffInDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.diffInWeek = function diffInWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.diffInMonth = function diffInMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.diffInQuarter = function diffInQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.diffInYear = function diffInYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.roundSecond = function roundSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.roundMinute = function roundMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.roundHour = function roundHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.roundDay = function roundDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.roundWeek = function roundWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.roundMonth = function roundMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.roundQuarter = function roundQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.roundYear = function roundYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return new Date(Math.round(t / step) * step);
  };
  lib.floorSecond = function floorSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.floorMinute = function floorMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.floorHour = function floorHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.floorDay = function floorDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.floorWeek = function floorWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.floorMonth = function floorMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.floorQuarter = function floorQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.floorYear = function floorYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return new Date(Math.floor(t / step) * step);
  };
  lib.ceilSecond = function ceilSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.ceilMinute = function ceilMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.ceilHour = function ceilHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.ceilDay = function ceilDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.ceilWeek = function ceilWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.ceilMonth = function ceilMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.ceilQuarter = function ceilQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.ceilYear = function ceilYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return new Date(Math.ceil(t / step) * step);
  };
  lib.clampSecond = function clampSecond(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.second * (amount === undefined ? 1 : amount);
    return new Date(Math.max(t / step) * step);
  };
  lib.clampMinute = function clampMinute(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.minute * (amount === undefined ? 1 : amount);
    return new Date(Math.max(t / step) * step);
  };
  lib.clampHour = function clampHour(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.hour * (amount === undefined ? 1 : amount);
    return new Date(Math.max(t / step) * step);
  };
  lib.clampDay = function clampDay(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.day * (amount === undefined ? 1 : amount);
    return new Date(Math.max(t / step) * step);
  };
  lib.clampWeek = function clampWeek(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.week * (amount === undefined ? 1 : amount);
    return new Date(Math.max(t / step) * step);
  };
  lib.clampMonth = function clampMonth(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.month * (amount === undefined ? 1 : amount);
    return new Date(Math.max(t / step) * step);
  };
  lib.clampQuarter = function clampQuarter(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.quarter * (amount === undefined ? 1 : amount);
    return new Date(Math.max(t / step) * step);
  };
  lib.clampYear = function clampYear(date, amount) {
    const t = date instanceof Date ? date.getTime() : Number(date);
    const step = MS.year * (amount === undefined ? 1 : amount);
    return new Date(Math.max(t / step) * step);
  };
  lib.relative_en_second = function (n) { return new Intl.RelativeTimeFormat("en", { numeric: "auto" }).format(n, "second"); };
  lib.relative_en_minute = function (n) { return new Intl.RelativeTimeFormat("en", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_en_hour = function (n) { return new Intl.RelativeTimeFormat("en", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_en_day = function (n) { return new Intl.RelativeTimeFormat("en", { numeric: "auto" }).format(n, "day"); };
  lib.relative_en_week = function (n) { return new Intl.RelativeTimeFormat("en", { numeric: "auto" }).format(n, "week"); };
  lib.relative_en_month = function (n) { return new Intl.RelativeTimeFormat("en", { numeric: "auto" }).format(n, "month"); };
  lib.relative_en_quarter = function (n) { return new Intl.RelativeTimeFormat("en", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_en_year = function (n) { return new Intl.RelativeTimeFormat("en", { numeric: "auto" }).format(n, "year"); };
  lib.months_en = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("en", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_fr_second = function (n) { return new Intl.RelativeTimeFormat("fr", { numeric: "auto" }).format(n, "second"); };
  lib.relative_fr_minute = function (n) { return new Intl.RelativeTimeFormat("fr", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_fr_hour = function (n) { return new Intl.RelativeTimeFormat("fr", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_fr_day = function (n) { return new Intl.RelativeTimeFormat("fr", { numeric: "auto" }).format(n, "day"); };
  lib.relative_fr_week = function (n) { return new Intl.RelativeTimeFormat("fr", { numeric: "auto" }).format(n, "week"); };
  lib.relative_fr_month = function (n) { return new Intl.RelativeTimeFormat("fr", { numeric: "auto" }).format(n, "month"); };
  lib.relative_fr_quarter = function (n) { return new Intl.RelativeTimeFormat("fr", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_fr_year = function (n) { return new Intl.RelativeTimeFormat("fr", { numeric: "auto" }).format(n, "year"); };
  lib.months_fr = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("fr", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_de_second = function (n) { return new Intl.RelativeTimeFormat("de", { numeric: "auto" }).format(n, "second"); };
  lib.relative_de_minute = function (n) { return new Intl.RelativeTimeFormat("de", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_de_hour = function (n) { return new Intl.RelativeTimeFormat("de", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_de_day = function (n) { return new Intl.RelativeTimeFormat("de", { numeric: "auto" }).format(n, "day"); };
  lib.relative_de_week = function (n) { return new Intl.RelativeTimeFormat("de", { numeric: "auto" }).format(n, "week"); };
  lib.relative_de_month = function (n) { return new Intl.RelativeTimeFormat("de", { numeric: "auto" }).format(n, "month"); };
  lib.relative_de_quarter = function (n) { return new Intl.RelativeTimeFormat("de", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_de_year = function (n) { return new Intl.RelativeTimeFormat("de", { numeric: "auto" }).format(n, "year"); };
  lib.months_de = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("de", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_es_second = function (n) { return new Intl.RelativeTimeFormat("es", { numeric: "auto" }).format(n, "second"); };
  lib.relative_es_minute = function (n) { return new Intl.RelativeTimeFormat("es", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_es_hour = function (n) { return new Intl.RelativeTimeFormat("es", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_es_day = function (n) { return new Intl.RelativeTimeFormat("es", { numeric: "auto" }).format(n, "day"); };
  lib.relative_es_week = function (n) { return new Intl.RelativeTimeFormat("es", { numeric: "auto" }).format(n, "week"); };
  lib.relative_es_month = function (n) { return new Intl.RelativeTimeFormat("es", { numeric: "auto" }).format(n, "month"); };
  lib.relative_es_quarter = function (n) { return new Intl.RelativeTimeFormat("es", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_es_year = function (n) { return new Intl.RelativeTimeFormat("es", { numeric: "auto" }).format(n, "year"); };
  lib.months_es = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("es", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_it_second = function (n) { return new Intl.RelativeTimeFormat("it", { numeric: "auto" }).format(n, "second"); };
  lib.relative_it_minute = function (n) { return new Intl.RelativeTimeFormat("it", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_it_hour = function (n) { return new Intl.RelativeTimeFormat("it", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_it_day = function (n) { return new Intl.RelativeTimeFormat("it", { numeric: "auto" }).format(n, "day"); };
  lib.relative_it_week = function (n) { return new Intl.RelativeTimeFormat("it", { numeric: "auto" }).format(n, "week"); };
  lib.relative_it_month = function (n) { return new Intl.RelativeTimeFormat("it", { numeric: "auto" }).format(n, "month"); };
  lib.relative_it_quarter = function (n) { return new Intl.RelativeTimeFormat("it", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_it_year = function (n) { return new Intl.RelativeTimeFormat("it", { numeric: "auto" }).format(n, "year"); };
  lib.months_it = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("it", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_pt_second = function (n) { return new Intl.RelativeTimeFormat("pt", { numeric: "auto" }).format(n, "second"); };
  lib.relative_pt_minute = function (n) { return new Intl.RelativeTimeFormat("pt", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_pt_hour = function (n) { return new Intl.RelativeTimeFormat("pt", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_pt_day = function (n) { return new Intl.RelativeTimeFormat("pt", { numeric: "auto" }).format(n, "day"); };
  lib.relative_pt_week = function (n) { return new Intl.RelativeTimeFormat("pt", { numeric: "auto" }).format(n, "week"); };
  lib.relative_pt_month = function (n) { return new Intl.RelativeTimeFormat("pt", { numeric: "auto" }).format(n, "month"); };
  lib.relative_pt_quarter = function (n) { return new Intl.RelativeTimeFormat("pt", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_pt_year = function (n) { return new Intl.RelativeTimeFormat("pt", { numeric: "auto" }).format(n, "year"); };
  lib.months_pt = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("pt", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_nl_second = function (n) { return new Intl.RelativeTimeFormat("nl", { numeric: "auto" }).format(n, "second"); };
  lib.relative_nl_minute = function (n) { return new Intl.RelativeTimeFormat("nl", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_nl_hour = function (n) { return new Intl.RelativeTimeFormat("nl", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_nl_day = function (n) { return new Intl.RelativeTimeFormat("nl", { numeric: "auto" }).format(n, "day"); };
  lib.relative_nl_week = function (n) { return new Intl.RelativeTimeFormat("nl", { numeric: "auto" }).format(n, "week"); };
  lib.relative_nl_month = function (n) { return new Intl.RelativeTimeFormat("nl", { numeric: "auto" }).format(n, "month"); };
  lib.relative_nl_quarter = function (n) { return new Intl.RelativeTimeFormat("nl", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_nl_year = function (n) { return new Intl.RelativeTimeFormat("nl", { numeric: "auto" }).format(n, "year"); };
  lib.months_nl = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("nl", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_sv_second = function (n) { return new Intl.RelativeTimeFormat("sv", { numeric: "auto" }).format(n, "second"); };
  lib.relative_sv_minute = function (n) { return new Intl.RelativeTimeFormat("sv", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_sv_hour = function (n) { return new Intl.RelativeTimeFormat("sv", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_sv_day = function (n) { return new Intl.RelativeTimeFormat("sv", { numeric: "auto" }).format(n, "day"); };
  lib.relative_sv_week = function (n) { return new Intl.RelativeTimeFormat("sv", { numeric: "auto" }).format(n, "week"); };
  lib.relative_sv_month = function (n) { return new Intl.RelativeTimeFormat("sv", { numeric: "auto" }).format(n, "month"); };
  lib.relative_sv_quarter = function (n) { return new Intl.RelativeTimeFormat("sv", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_sv_year = function (n) { return new Intl.RelativeTimeFormat("sv", { numeric: "auto" }).format(n, "year"); };
  lib.months_sv = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("sv", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_pl_second = function (n) { return new Intl.RelativeTimeFormat("pl", { numeric: "auto" }).format(n, "second"); };
  lib.relative_pl_minute = function (n) { return new Intl.RelativeTimeFormat("pl", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_pl_hour = function (n) { return new Intl.RelativeTimeFormat("pl", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_pl_day = function (n) { return new Intl.RelativeTimeFormat("pl", { numeric: "auto" }).format(n, "day"); };
  lib.relative_pl_week = function (n) { return new Intl.RelativeTimeFormat("pl", { numeric: "auto" }).format(n, "week"); };
  lib.relative_pl_month = function (n) { return new Intl.RelativeTimeFormat("pl", { numeric: "auto" }).format(n, "month"); };
  lib.relative_pl_quarter = function (n) { return new Intl.RelativeTimeFormat("pl", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_pl_year = function (n) { return new Intl.RelativeTimeFormat("pl", { numeric: "auto" }).format(n, "year"); };
  lib.months_pl = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("pl", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_tr_second = function (n) { return new Intl.RelativeTimeFormat("tr", { numeric: "auto" }).format(n, "second"); };
  lib.relative_tr_minute = function (n) { return new Intl.RelativeTimeFormat("tr", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_tr_hour = function (n) { return new Intl.RelativeTimeFormat("tr", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_tr_day = function (n) { return new Intl.RelativeTimeFormat("tr", { numeric: "auto" }).format(n, "day"); };
  lib.relative_tr_week = function (n) { return new Intl.RelativeTimeFormat("tr", { numeric: "auto" }).format(n, "week"); };
  lib.relative_tr_month = function (n) { return new Intl.RelativeTimeFormat("tr", { numeric: "auto" }).format(n, "month"); };
  lib.relative_tr_quarter = function (n) { return new Intl.RelativeTimeFormat("tr", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_tr_year = function (n) { return new Intl.RelativeTimeFormat("tr", { numeric: "auto" }).format(n, "year"); };
  lib.months_tr = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("tr", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_ja_second = function (n) { return new Intl.RelativeTimeFormat("ja", { numeric: "auto" }).format(n, "second"); };
  lib.relative_ja_minute = function (n) { return new Intl.RelativeTimeFormat("ja", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_ja_hour = function (n) { return new Intl.RelativeTimeFormat("ja", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_ja_day = function (n) { return new Intl.RelativeTimeFormat("ja", { numeric: "auto" }).format(n, "day"); };
  lib.relative_ja_week = function (n) { return new Intl.RelativeTimeFormat("ja", { numeric: "auto" }).format(n, "week"); };
  lib.relative_ja_month = function (n) { return new Intl.RelativeTimeFormat("ja", { numeric: "auto" }).format(n, "month"); };
  lib.relative_ja_quarter = function (n) { return new Intl.RelativeTimeFormat("ja", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_ja_year = function (n) { return new Intl.RelativeTimeFormat("ja", { numeric: "auto" }).format(n, "year"); };
  lib.months_ja = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("ja", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_ko_second = function (n) { return new Intl.RelativeTimeFormat("ko", { numeric: "auto" }).format(n, "second"); };
  lib.relative_ko_minute = function (n) { return new Intl.RelativeTimeFormat("ko", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_ko_hour = function (n) { return new Intl.RelativeTimeFormat("ko", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_ko_day = function (n) { return new Intl.RelativeTimeFormat("ko", { numeric: "auto" }).format(n, "day"); };
  lib.relative_ko_week = function (n) { return new Intl.RelativeTimeFormat("ko", { numeric: "auto" }).format(n, "week"); };
  lib.relative_ko_month = function (n) { return new Intl.RelativeTimeFormat("ko", { numeric: "auto" }).format(n, "month"); };
  lib.relative_ko_quarter = function (n) { return new Intl.RelativeTimeFormat("ko", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_ko_year = function (n) { return new Intl.RelativeTimeFormat("ko", { numeric: "auto" }).format(n, "year"); };
  lib.months_ko = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("ko", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_zh_second = function (n) { return new Intl.RelativeTimeFormat("zh", { numeric: "auto" }).format(n, "second"); };
  lib.relative_zh_minute = function (n) { return new Intl.RelativeTimeFormat("zh", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_zh_hour = function (n) { return new Intl.RelativeTimeFormat("zh", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_zh_day = function (n) { return new Intl.RelativeTimeFormat("zh", { numeric: "auto" }).format(n, "day"); };
  lib.relative_zh_week = function (n) { return new Intl.RelativeTimeFormat("zh", { numeric: "auto" }).format(n, "week"); };
  lib.relative_zh_month = function (n) { return new Intl.RelativeTimeFormat("zh", { numeric: "auto" }).format(n, "month"); };
  lib.relative_zh_quarter = function (n) { return new Intl.RelativeTimeFormat("zh", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_zh_year = function (n) { return new Intl.RelativeTimeFormat("zh", { numeric: "auto" }).format(n, "year"); };
  lib.months_zh = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("zh", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_hi_second = function (n) { return new Intl.RelativeTimeFormat("hi", { numeric: "auto" }).format(n, "second"); };
  lib.relative_hi_minute = function (n) { return new Intl.RelativeTimeFormat("hi", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_hi_hour = function (n) { return new Intl.RelativeTimeFormat("hi", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_hi_day = function (n) { return new Intl.RelativeTimeFormat("hi", { numeric: "auto" }).format(n, "day"); };
  lib.relative_hi_week = function (n) { return new Intl.RelativeTimeFormat("hi", { numeric: "auto" }).format(n, "week"); };
  lib.relative_hi_month = function (n) { return new Intl.RelativeTimeFormat("hi", { numeric: "auto" }).format(n, "month"); };
  lib.relative_hi_quarter = function (n) { return new Intl.RelativeTimeFormat("hi", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_hi_year = function (n) { return new Intl.RelativeTimeFormat("hi", { numeric: "auto" }).format(n, "year"); };
  lib.months_hi = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("hi", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_ar_second = function (n) { return new Intl.RelativeTimeFormat("ar", { numeric: "auto" }).format(n, "second"); };
  lib.relative_ar_minute = function (n) { return new Intl.RelativeTimeFormat("ar", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_ar_hour = function (n) { return new Intl.RelativeTimeFormat("ar", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_ar_day = function (n) { return new Intl.RelativeTimeFormat("ar", { numeric: "auto" }).format(n, "day"); };
  lib.relative_ar_week = function (n) { return new Intl.RelativeTimeFormat("ar", { numeric: "auto" }).format(n, "week"); };
  lib.relative_ar_month = function (n) { return new Intl.RelativeTimeFormat("ar", { numeric: "auto" }).format(n, "month"); };
  lib.relative_ar_quarter = function (n) { return new Intl.RelativeTimeFormat("ar", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_ar_year = function (n) { return new Intl.RelativeTimeFormat("ar", { numeric: "auto" }).format(n, "year"); };
  lib.months_ar = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("ar", { month: "long" }).format(new Date(2024, m, 1)));
  lib.relative_ru_second = function (n) { return new Intl.RelativeTimeFormat("ru", { numeric: "auto" }).format(n, "second"); };
  lib.relative_ru_minute = function (n) { return new Intl.RelativeTimeFormat("ru", { numeric: "auto" }).format(n, "minute"); };
  lib.relative_ru_hour = function (n) { return new Intl.RelativeTimeFormat("ru", { numeric: "auto" }).format(n, "hour"); };
  lib.relative_ru_day = function (n) { return new Intl.RelativeTimeFormat("ru", { numeric: "auto" }).format(n, "day"); };
  lib.relative_ru_week = function (n) { return new Intl.RelativeTimeFormat("ru", { numeric: "auto" }).format(n, "week"); };
  lib.relative_ru_month = function (n) { return new Intl.RelativeTimeFormat("ru", { numeric: "auto" }).format(n, "month"); };
  lib.relative_ru_quarter = function (n) { return new Intl.RelativeTimeFormat("ru", { numeric: "auto" }).format(n, "quarter"); };
  lib.relative_ru_year = function (n) { return new Intl.RelativeTimeFormat("ru", { numeric: "auto" }).format(n, "year"); };
  lib.months_ru = Array.from({ length: 12 }, (_, m) => new Intl.DateTimeFormat("ru", { month: "long" }).format(new Date(2024, m, 1)));
  Object.assign(global, lib);
})(window);
