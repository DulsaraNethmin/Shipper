// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'open_jobs_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$OpenJobsState {

/// The **first** page is being read, or a retry after a failure is.
///
/// Deliberately not set by a pull-to-refresh: `RefreshIndicator` draws its own spinner, and
/// replacing the feed with a second one would take the work away from somebody who pulled
/// precisely to look at it.
 bool get loading;/// A further page is being read.
 bool get loadingMore;/// Every job read so far, newest first, in the platform's own order.
 List<OpenJob> get jobs;/// Whether a page has arrived at all.
///
/// This is what separates "no work right now" from "not yet asked", and the separation is the
/// whole of the empty state: a provider with nothing eligible must see an empty state and
/// never a spinner that does not resolve.
 bool get loaded;/// The position to ask from next. **Opaque** — passed back exactly as it arrived.
 String? get nextCursor;/// Whether asking again would return anything.
 bool get hasMore;/// How the provider has narrowed their own feed.
///
/// **State of the screen, not of the request.** Nothing here is ever sent: the endpoint takes
/// no filter, and this only hides jobs the platform already offered. It survives a refresh and
/// a further page on purpose — a provider who narrowed to Victoria and pulled to refresh asked
/// to see Victoria again, not to have their choice quietly discarded.
 OpenJobsFilter get filter;/// What the last read failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of OpenJobsState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$OpenJobsStateCopyWith<OpenJobsState> get copyWith => _$OpenJobsStateCopyWithImpl<OpenJobsState>(this as OpenJobsState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is OpenJobsState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&const DeepCollectionEquality().equals(other.jobs, jobs)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.filter, filter) || other.filter == filter)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loadingMore,const DeepCollectionEquality().hash(jobs),loaded,nextCursor,hasMore,filter,failure);

@override
String toString() {
  return 'OpenJobsState(loading: $loading, loadingMore: $loadingMore, jobs: $jobs, loaded: $loaded, nextCursor: $nextCursor, hasMore: $hasMore, filter: $filter, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $OpenJobsStateCopyWith<$Res>  {
  factory $OpenJobsStateCopyWith(OpenJobsState value, $Res Function(OpenJobsState) _then) = _$OpenJobsStateCopyWithImpl;
@useResult
$Res call({
 bool loading, bool loadingMore, List<OpenJob> jobs, bool loaded, String? nextCursor, bool hasMore, OpenJobsFilter filter, ApiFailure? failure
});




}
/// @nodoc
class _$OpenJobsStateCopyWithImpl<$Res>
    implements $OpenJobsStateCopyWith<$Res> {
  _$OpenJobsStateCopyWithImpl(this._self, this._then);

  final OpenJobsState _self;
  final $Res Function(OpenJobsState) _then;

/// Create a copy of OpenJobsState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? loading = null,Object? loadingMore = null,Object? jobs = null,Object? loaded = null,Object? nextCursor = freezed,Object? hasMore = null,Object? filter = null,Object? failure = freezed,}) {
  return _then(_self.copyWith(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,jobs: null == jobs ? _self.jobs : jobs // ignore: cast_nullable_to_non_nullable
as List<OpenJob>,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,filter: null == filter ? _self.filter : filter // ignore: cast_nullable_to_non_nullable
as OpenJobsFilter,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

}


/// Adds pattern-matching-related methods to [OpenJobsState].
extension OpenJobsStatePatterns on OpenJobsState {
/// A variant of `map` that fallback to returning `orElse`.
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case final Subclass value:
///     return ...;
///   case _:
///     return orElse();
/// }
/// ```

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _OpenJobsState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _OpenJobsState() when $default != null:
return $default(_that);case _:
  return orElse();

}
}
/// A `switch`-like method, using callbacks.
///
/// Callbacks receives the raw object, upcasted.
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case final Subclass value:
///     return ...;
///   case final Subclass2 value:
///     return ...;
/// }
/// ```

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _OpenJobsState value)  $default,){
final _that = this;
switch (_that) {
case _OpenJobsState():
return $default(_that);case _:
  throw StateError('Unexpected subclass');

}
}
/// A variant of `map` that fallback to returning `null`.
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case final Subclass value:
///     return ...;
///   case _:
///     return null;
/// }
/// ```

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _OpenJobsState value)?  $default,){
final _that = this;
switch (_that) {
case _OpenJobsState() when $default != null:
return $default(_that);case _:
  return null;

}
}
/// A variant of `when` that fallback to an `orElse` callback.
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case Subclass(:final field):
///     return ...;
///   case _:
///     return orElse();
/// }
/// ```

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool loading,  bool loadingMore,  List<OpenJob> jobs,  bool loaded,  String? nextCursor,  bool hasMore,  OpenJobsFilter filter,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _OpenJobsState() when $default != null:
return $default(_that.loading,_that.loadingMore,_that.jobs,_that.loaded,_that.nextCursor,_that.hasMore,_that.filter,_that.failure);case _:
  return orElse();

}
}
/// A `switch`-like method, using callbacks.
///
/// As opposed to `map`, this offers destructuring.
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case Subclass(:final field):
///     return ...;
///   case Subclass2(:final field2):
///     return ...;
/// }
/// ```

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool loading,  bool loadingMore,  List<OpenJob> jobs,  bool loaded,  String? nextCursor,  bool hasMore,  OpenJobsFilter filter,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _OpenJobsState():
return $default(_that.loading,_that.loadingMore,_that.jobs,_that.loaded,_that.nextCursor,_that.hasMore,_that.filter,_that.failure);case _:
  throw StateError('Unexpected subclass');

}
}
/// A variant of `when` that fallback to returning `null`
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case Subclass(:final field):
///     return ...;
///   case _:
///     return null;
/// }
/// ```

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool loading,  bool loadingMore,  List<OpenJob> jobs,  bool loaded,  String? nextCursor,  bool hasMore,  OpenJobsFilter filter,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _OpenJobsState() when $default != null:
return $default(_that.loading,_that.loadingMore,_that.jobs,_that.loaded,_that.nextCursor,_that.hasMore,_that.filter,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _OpenJobsState extends OpenJobsState {
  const _OpenJobsState({this.loading = true, this.loadingMore = false, final  List<OpenJob> jobs = const <OpenJob>[], this.loaded = false, this.nextCursor, this.hasMore = false, this.filter = const OpenJobsFilter(), this.failure}): _jobs = jobs,super._();
  

/// The **first** page is being read, or a retry after a failure is.
///
/// Deliberately not set by a pull-to-refresh: `RefreshIndicator` draws its own spinner, and
/// replacing the feed with a second one would take the work away from somebody who pulled
/// precisely to look at it.
@override@JsonKey() final  bool loading;
/// A further page is being read.
@override@JsonKey() final  bool loadingMore;
/// Every job read so far, newest first, in the platform's own order.
 final  List<OpenJob> _jobs;
/// Every job read so far, newest first, in the platform's own order.
@override@JsonKey() List<OpenJob> get jobs {
  if (_jobs is EqualUnmodifiableListView) return _jobs;
  // ignore: implicit_dynamic_type
  return EqualUnmodifiableListView(_jobs);
}

/// Whether a page has arrived at all.
///
/// This is what separates "no work right now" from "not yet asked", and the separation is the
/// whole of the empty state: a provider with nothing eligible must see an empty state and
/// never a spinner that does not resolve.
@override@JsonKey() final  bool loaded;
/// The position to ask from next. **Opaque** — passed back exactly as it arrived.
@override final  String? nextCursor;
/// Whether asking again would return anything.
@override@JsonKey() final  bool hasMore;
/// How the provider has narrowed their own feed.
///
/// **State of the screen, not of the request.** Nothing here is ever sent: the endpoint takes
/// no filter, and this only hides jobs the platform already offered. It survives a refresh and
/// a further page on purpose — a provider who narrowed to Victoria and pulled to refresh asked
/// to see Victoria again, not to have their choice quietly discarded.
@override@JsonKey() final  OpenJobsFilter filter;
/// What the last read failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of OpenJobsState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$OpenJobsStateCopyWith<_OpenJobsState> get copyWith => __$OpenJobsStateCopyWithImpl<_OpenJobsState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _OpenJobsState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&const DeepCollectionEquality().equals(other._jobs, _jobs)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.filter, filter) || other.filter == filter)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loadingMore,const DeepCollectionEquality().hash(_jobs),loaded,nextCursor,hasMore,filter,failure);

@override
String toString() {
  return 'OpenJobsState(loading: $loading, loadingMore: $loadingMore, jobs: $jobs, loaded: $loaded, nextCursor: $nextCursor, hasMore: $hasMore, filter: $filter, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$OpenJobsStateCopyWith<$Res> implements $OpenJobsStateCopyWith<$Res> {
  factory _$OpenJobsStateCopyWith(_OpenJobsState value, $Res Function(_OpenJobsState) _then) = __$OpenJobsStateCopyWithImpl;
@override @useResult
$Res call({
 bool loading, bool loadingMore, List<OpenJob> jobs, bool loaded, String? nextCursor, bool hasMore, OpenJobsFilter filter, ApiFailure? failure
});




}
/// @nodoc
class __$OpenJobsStateCopyWithImpl<$Res>
    implements _$OpenJobsStateCopyWith<$Res> {
  __$OpenJobsStateCopyWithImpl(this._self, this._then);

  final _OpenJobsState _self;
  final $Res Function(_OpenJobsState) _then;

/// Create a copy of OpenJobsState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? loading = null,Object? loadingMore = null,Object? jobs = null,Object? loaded = null,Object? nextCursor = freezed,Object? hasMore = null,Object? filter = null,Object? failure = freezed,}) {
  return _then(_OpenJobsState(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,jobs: null == jobs ? _self._jobs : jobs // ignore: cast_nullable_to_non_nullable
as List<OpenJob>,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,filter: null == filter ? _self.filter : filter // ignore: cast_nullable_to_non_nullable
as OpenJobsFilter,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}


}

// dart format on
